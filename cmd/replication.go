package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"github.com/zrepl/zrepl/jobrun"
	"github.com/zrepl/zrepl/rpc"
	"github.com/zrepl/zrepl/zfs"
	"io"
	"os"
	"sync"
	"time"
)

var runArgs struct {
	job  string
	once bool
}

var RunCmd = &cobra.Command{
	Use:   "run",
	Short: "run push & pull replication",
	Run:   cmdRun,
}

var PushCmd = &cobra.Command{
	Use:   "push",
	Short: "run push job (first positional argument)",
	Run:   cmdPush,
}

var PullCmd = &cobra.Command{
	Use:   "pull",
	Short: "run pull job (first positional argument)",
	Run:   cmdPull,
}

func init() {
	RootCmd.AddCommand(RunCmd)
	RunCmd.Flags().BoolVar(&runArgs.once, "once", false, "run jobs only once, regardless of configured repeat behavior")
	RunCmd.Flags().StringVar(&runArgs.job, "job", "", "run only the given job")

	RootCmd.AddCommand(PushCmd)
	RootCmd.AddCommand(PullCmd)
}

func cmdPush(cmd *cobra.Command, args []string) {

	if len(args) != 1 {
		log.Printf("must specify exactly one job as positional argument")
		os.Exit(1)
	}
	job, ok := conf.Pushs[args[0]]
	if !ok {
		log.Printf("could not find push job %s", args[0])
		os.Exit(1)
	}
	if err := jobPush(job, log); err != nil {
		log.Printf("error doing push: %s", err)
		os.Exit(1)
	}

}

func cmdPull(cmd *cobra.Command, args []string) {

	if len(args) != 1 {
		log.Printf("must specify exactly one job as positional argument")
		os.Exit(1)
	}
	job, ok := conf.Pulls[args[0]]
	if !ok {
		log.Printf("could not find pull job %s", args[0])
		os.Exit(1)
	}
	if err := jobPull(job, log); err != nil {
		log.Printf("error doing pull: %s", err)
		os.Exit(1)
	}

}

func cmdRun(cmd *cobra.Command, args []string) {

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runner.Start()
	}()

	jobs := make([]jobrun.Job, len(conf.Pulls)+len(conf.Pushs))
	i := 0
	for _, pull := range conf.Pulls {
		jobs[i] = jobrun.Job{
			Name:           fmt.Sprintf("pull.%d", i),
			RepeatStrategy: pull.RepeatStrategy,
			RunFunc: func(log jobrun.Logger) error {
				log.Printf("doing pull: %v", pull)
				return jobPull(pull, log)
			},
		}
		i++
	}
	for _, push := range conf.Pushs {
		jobs[i] = jobrun.Job{
			Name:           fmt.Sprintf("push.%d", i),
			RepeatStrategy: push.RepeatStrategy,
			RunFunc: func(log jobrun.Logger) error {
				log.Printf("doing push: %v", push)
				return jobPush(push, log)
			},
		}
		i++
	}

	for _, j := range jobs {
		if runArgs.once {
			j.RepeatStrategy = jobrun.NoRepeatStrategy{}
		}
		if runArgs.job != "" {
			if runArgs.job == j.Name {
				runner.AddJob(j)
				break
			}
			continue
		}
		runner.AddJob(j)
	}

	for {
		select {
		case job := <-runner.NotificationChan():
			log.Printf("job %s reported error: %v\n", job.Name, job.LastError)
		}
	}

	wg.Wait()

}

func jobPull(pull *Pull, log jobrun.Logger) (err error) {

	if lt, ok := pull.From.Transport.(LocalTransport); ok {
		lt.SetHandler(Handler{
			Logger:  log,
			PullACL: pull.Mapping,
		})
		pull.From.Transport = lt
		log.Printf("fixing up local transport: %#v", pull.From.Transport)
	}

	var remote rpc.RPCRequester

	if remote, err = pull.From.Transport.Connect(log); err != nil {
		return
	}

	defer closeRPCWithTimeout(log, remote, time.Second*10, "")

	return doPull(PullContext{remote, log, pull.Mapping, pull.InitialReplPolicy})
}

func jobPush(push *Push, log jobrun.Logger) (err error) {

	if _, ok := push.To.Transport.(LocalTransport); ok {
		panic("no support for local pushs")
	}

	var remote rpc.RPCRequester
	if remote, err = push.To.Transport.Connect(log); err != nil {
		return err
	}

	defer closeRPCWithTimeout(log, remote, time.Second*10, "")

	log.Printf("building handler for PullMeRequest")
	handler := Handler{
		Logger:          log,
		PullACL:         push.Filter,
		SinkMappingFunc: nil, // no need for that in the handler for PullMe
	}
	log.Printf("handler: %#v", handler)

	r := rpc.PullMeRequest{
		InitialReplPolicy: push.InitialReplPolicy,
	}
	log.Printf("doing PullMeRequest: %#v", r)

	if err = remote.PullMeRequest(r, handler); err != nil {
		log.Printf("PullMeRequest failed: %s", err)
		return
	}

	log.Printf("push job finished")
	return

}

func closeRPCWithTimeout(log Logger, remote rpc.RPCRequester, timeout time.Duration, goodbye string) {
	log.Printf("closing rpc connection")

	ch := make(chan error)
	go func() {
		ch <- remote.CloseRequest(rpc.CloseRequest{goodbye})
	}()

	var err error
	select {
	case <-time.After(timeout):
		err = fmt.Errorf("timeout exceeded (%s)", timeout)
	case closeRequestErr := <-ch:
		err = closeRequestErr
	}

	if err != nil {
		log.Printf("error closing connection: %s", err)
		err = remote.ForceClose()
		if err != nil {
			log.Printf("error force-closing connection: %s", err)
		}
	}
	return
}

type PullContext struct {
	Remote            rpc.RPCRequester
	Log               Logger
	Mapping           zfs.DatasetMapping
	InitialReplPolicy rpc.InitialReplPolicy
}

func doPull(pull PullContext) (err error) {

	remote := pull.Remote
	log := pull.Log

	fsr := rpc.FilesystemRequest{}
	var remoteFilesystems []zfs.DatasetPath
	if remoteFilesystems, err = remote.FilesystemRequest(fsr); err != nil {
		return
	}

	type RemoteLocalMapping struct {
		Remote      zfs.DatasetPath
		Local       zfs.DatasetPath
		LocalExists bool
	}
	replMapping := make(map[string]RemoteLocalMapping, len(remoteFilesystems))
	localTraversal := zfs.NewDatasetPathForest()
	localExists, err := zfs.ZFSListFilesystemExists()
	if err != nil {
		log.Printf("cannot get local filesystems map: %s", err)
		return err
	}

	{

		log.Printf("mapping using %#v\n", pull.Mapping)
		for fs := range remoteFilesystems {
			var err error
			var localFs zfs.DatasetPath
			localFs, err = pull.Mapping.Map(remoteFilesystems[fs])
			if err != nil {
				if err != zfs.NoMatchError {
					log.Printf("error mapping %s: %#v\n", remoteFilesystems[fs], err)
					return err
				}
				continue
			}
			m := RemoteLocalMapping{remoteFilesystems[fs], localFs, localExists(localFs)}
			replMapping[m.Local.ToString()] = m
			localTraversal.Add(m.Local)
		}

	}

	log.Printf("remoteFilesystems: %#v\nreplMapping: %#v\n", remoteFilesystems, replMapping)

	// per fs sync, assume sorted in top-down order TODO

	localTraversal.WalkTopDown(func(v zfs.DatasetPathVisit) bool {

		if v.FilledIn {
			if localExists(v.Path) {
				return true
			}
			log.Printf("aborting, don't know how to create fill-in dataset %s", v.Path)
			err = fmt.Errorf("aborting, don't know how to create fill-in dataset: %s", v.Path)
			return false
		}

		m, ok := replMapping[v.Path.ToString()]
		if !ok {
			panic("internal inconsistency: replMapping should contain mapping for any path that was not filled in by WalkTopDown()")
		}

		log := func(format string, args ...interface{}) {
			log.Printf("[%s => %s]: %s", m.Remote.ToString(), m.Local.ToString(), fmt.Sprintf(format, args...))
		}

		log("mapping: %#v\n", m)

		var versions []zfs.FilesystemVersion
		if m.LocalExists {
			if versions, err = zfs.ZFSListFilesystemVersions(m.Local, nil); err != nil {
				log("cannot get filesystem versions, stopping...: %v\n", m.Local.ToString(), m, err)
				return false
			}
		}

		var theirVersions []zfs.FilesystemVersion
		theirVersions, err = remote.FilesystemVersionsRequest(rpc.FilesystemVersionsRequest{
			Filesystem: m.Remote,
		})
		if err != nil {
			log("cannot fetch remote filesystem versions, stopping: %s", err)
			return false
		}

		diff := zfs.MakeFilesystemDiff(versions, theirVersions)
		log("diff: %#v\n", diff)

		switch diff.Conflict {
		case zfs.ConflictAllRight:

			log("performing initial sync, following policy: %#v", pull.InitialReplPolicy)

			if pull.InitialReplPolicy != rpc.InitialReplPolicyMostRecent {
				panic(fmt.Sprintf("policy %#v not implemented", pull.InitialReplPolicy))
			}

			snapsOnly := make([]zfs.FilesystemVersion, 0, len(diff.MRCAPathRight))
			for s := range diff.MRCAPathRight {
				if diff.MRCAPathRight[s].Type == zfs.Snapshot {
					snapsOnly = append(snapsOnly, diff.MRCAPathRight[s])
				}
			}

			if len(snapsOnly) < 1 {
				log("cannot perform initial sync: no remote snapshots. stopping...")
				return false
			}

			r := rpc.InitialTransferRequest{
				Filesystem:        m.Remote,
				FilesystemVersion: snapsOnly[len(snapsOnly)-1],
			}

			log("requesting initial transfer")

			var stream io.Reader
			if stream, err = remote.InitialTransferRequest(r); err != nil {
				log("error initial transfer request, stopping...: %s", err)
				return false
			}

			log("received initial transfer request response. zfs recv...")

			if err = zfs.ZFSRecv(m.Local, stream, "-u"); err != nil {
				log("error receiving stream, stopping...: %s", err)
				return false
			}

			log("configuring properties of received filesystem")

			if err = zfs.ZFSSet(m.Local, "readonly", "on"); err != nil {

			}

			log("finished initial transfer")
			return true

		case zfs.ConflictIncremental:

			if len(diff.IncrementalPath) < 2 {
				log("remote and local are in sync")
				return true
			}

			log("incremental transfers using path: %#v", diff.IncrementalPath)

			for i := 0; i < len(diff.IncrementalPath)-1; i++ {

				from, to := diff.IncrementalPath[i], diff.IncrementalPath[i+1]

				log := func(format string, args ...interface{}) {
					log("[%s => %s]: %s", from.Name, to.Name, fmt.Sprintf(format, args...))
				}

				r := rpc.IncrementalTransferRequest{
					Filesystem: m.Remote,
					From:       from,
					To:         to,
				}
				log("requesting incremental transfer: %#v", r)

				var stream io.Reader
				if stream, err = remote.IncrementalTransferRequest(r); err != nil {
					log("error requesting incremental transfer, stopping...: %s", err.Error())
					return false
				}

				log("receving incremental transfer")

				if err = zfs.ZFSRecv(m.Local, stream); err != nil {
					log("error receiving stream, stopping...: %s", err)
					return false
				}

				log("finished incremental transfer")

			}

			log("finished incremental transfer path")
			return true

		case zfs.ConflictNoCommonAncestor:

			log("sender and receiver filesystem have snapshots, but no common one")
			log("perform manual replication to establish a common snapshot history")
			log("sender snapshot list: %#v", diff.MRCAPathRight)
			log("receiver snapshot list: %#v", diff.MRCAPathLeft)
			return false

		case zfs.ConflictDiverged:

			log("sender and receiver filesystem share a history but have diverged")
			log("perform manual replication or delete snapshots on the receiving" +
				"side  to establish an incremental replication parse")
			log("sender-only snapshots: %#v", diff.MRCAPathRight)
			log("receiver-only snapshots: %#v", diff.MRCAPathLeft)
			return false

		}

		panic("implementation error: this should not be reached")
		return false

	})

	return

}
