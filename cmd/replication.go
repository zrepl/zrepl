package cmd

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/zrepl/zrepl/jobrun"
	"github.com/zrepl/zrepl/rpc"
	"github.com/zrepl/zrepl/util"
	"github.com/zrepl/zrepl/zfs"
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

type localPullACL struct{}

func (a localPullACL) Filter(p *zfs.DatasetPath) (pass bool, err error) {
	return true, nil
}

const LOCAL_TRANSPORT_IDENTITY string = "local"

const DEFAULT_INITIAL_REPL_POLICY = InitialReplPolicyMostRecent

type InitialReplPolicy string

const (
	InitialReplPolicyMostRecent InitialReplPolicy = "most_recent"
	InitialReplPolicyAll        InitialReplPolicy = "all"
)

func jobPull(pull *Pull, log jobrun.Logger) (err error) {

	var remote rpc.RPCClient

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

	var remote rpc.RPCClient
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

	panic("no support for push atm")

	log.Printf("push job finished")
	return

}

func closeRPCWithTimeout(log Logger, remote rpc.RPCClient, timeout time.Duration, goodbye string) {
	log.Printf("closing rpc connection")

	ch := make(chan error)
	go func() {
		ch <- remote.Close()
		close(ch)
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
	}
	return
}

type PullContext struct {
	Remote            rpc.RPCClient
	Log               Logger
	Mapping           DatasetMapping
	InitialReplPolicy InitialReplPolicy
}

func doPull(pull PullContext) (err error) {

	remote := pull.Remote
	log := pull.Log

	log.Printf("requesting remote filesystem list")
	fsr := FilesystemRequest{}
	var remoteFilesystems []*zfs.DatasetPath
	if err = remote.Call("FilesystemRequest", &fsr, &remoteFilesystems); err != nil {
		return
	}

	log.Printf("map remote filesystems to local paths and determine order for per-filesystem sync")
	type RemoteLocalMapping struct {
		Remote *zfs.DatasetPath
		Local  *zfs.DatasetPath
	}
	replMapping := make(map[string]RemoteLocalMapping, len(remoteFilesystems))
	localTraversal := zfs.NewDatasetPathForest()
	for fs := range remoteFilesystems {
		var err error
		var localFs *zfs.DatasetPath
		localFs, err = pull.Mapping.Map(remoteFilesystems[fs])
		if err != nil {
			if err != NoMatchError {
				err := fmt.Errorf("error mapping %s: %s", remoteFilesystems[fs], err)
				log.Printf("%s", err)
				return err
			}
			continue
		}
		log.Printf("%s => %s", remoteFilesystems[fs].ToString(), localFs.ToString())
		m := RemoteLocalMapping{remoteFilesystems[fs], localFs}
		replMapping[m.Local.ToString()] = m
		localTraversal.Add(m.Local)
	}

	log.Printf("build cache for already present local filesystem state")
	localFilesystemState, err := zfs.ZFSListFilesystemState()
	if err != nil {
		log.Printf("error requesting local filesystem state: %s", err)
		return err
	}

	log.Printf("start per-filesystem sync")
	localTraversal.WalkTopDown(func(v zfs.DatasetPathVisit) bool {

		if v.FilledIn {
			if _, exists := localFilesystemState[v.Path.ToString()]; exists {
				// No need to verify if this is a placeholder or not. It is sufficient
				// to know we can add child filesystems to it
				return true
			}
			log.Printf("creating placeholder filesystem %s", v.Path.ToString())
			err = zfs.ZFSCreatePlaceholderFilesystem(v.Path)
			if err != nil {
				err = fmt.Errorf("aborting, cannot create placeholder filesystem %s: %s", v.Path, err)
				return false
			}
			return true
		}

		m, ok := replMapping[v.Path.ToString()]
		if !ok {
			panic("internal inconsistency: replMapping should contain mapping for any path that was not filled in by WalkTopDown()")
		}

		log := func(format string, args ...interface{}) {
			log.Printf("[%s => %s]: %s", m.Remote.ToString(), m.Local.ToString(), fmt.Sprintf(format, args...))
		}

		log("examing local filesystem state")
		localState, localExists := localFilesystemState[m.Local.ToString()]
		var versions []zfs.FilesystemVersion
		switch {
		case !localExists:
			log("local filesystem does not exist")
		case localState.Placeholder:
			log("local filesystem is marked as placeholder")
		default:
			log("local filesystem exists")
			log("requesting local filesystem versions")
			if versions, err = zfs.ZFSListFilesystemVersions(m.Local, nil); err != nil {
				log("cannot get local filesystem versions: %s", err)
				return false
			}
		}

		log("requesting remote filesystem versions")
		r := FilesystemVersionsRequest{
			Filesystem: m.Remote,
		}
		var theirVersions []zfs.FilesystemVersion
		if err = remote.Call("FilesystemVersionsRequest", &r, &theirVersions); err != nil {
			log("error requesting remote filesystem versions: %s", err)
			log("stopping replication for all filesystems mapped as children of %s", m.Local.ToString())
			return false
		}

		log("computing diff between remote and local filesystem versions")
		diff := zfs.MakeFilesystemDiff(versions, theirVersions)
		log("%s", diff)

		if localState.Placeholder && diff.Conflict != zfs.ConflictAllRight {
			panic("internal inconsistency: local placeholder implies ConflictAllRight")
		}

		switch diff.Conflict {
		case zfs.ConflictAllRight:

			log("performing initial sync, following policy: '%s'", pull.InitialReplPolicy)

			if pull.InitialReplPolicy != InitialReplPolicyMostRecent {
				panic(fmt.Sprintf("policy '%s' not implemented", pull.InitialReplPolicy))
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

			r := InitialTransferRequest{
				Filesystem:        m.Remote,
				FilesystemVersion: snapsOnly[len(snapsOnly)-1],
			}

			log("requesting snapshot stream for %s", r.FilesystemVersion)

			var stream io.Reader

			if err = remote.Call("InitialTransferRequest", &r, &stream); err != nil {
				log("error requesting initial transfer: %s", err)
				return false
			}
			log("received initial transfer request response")

			log("invoking zfs receive")
			watcher := util.IOProgressWatcher{Reader: stream}
			watcher.KickOff(1*time.Second, func(p util.IOProgress) {
				log("progress on receive operation: %v bytes received", p.TotalRX)
			})

			recvArgs := []string{"-u"}
			if localState.Placeholder {
				log("receive with forced rollback to replace placeholder filesystem")
				recvArgs = append(recvArgs, "-F")
			}

			if err = zfs.ZFSRecv(m.Local, &watcher, recvArgs...); err != nil {
				log("error receiving stream: %s", err)
				return false
			}
			log("finished receiving stream, %v bytes total", watcher.Progress().TotalRX)

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

			log("following incremental path from diff")
			var pathRx uint64

			for i := 0; i < len(diff.IncrementalPath)-1; i++ {

				from, to := diff.IncrementalPath[i], diff.IncrementalPath[i+1]

				log := func(format string, args ...interface{}) {
					log("[%v/%v][%s => %s]: %s", i+1, len(diff.IncrementalPath)-1,
						from.Name, to.Name, fmt.Sprintf(format, args...))
				}

				log("requesting incremental snapshot stream")
				r := IncrementalTransferRequest{
					Filesystem: m.Remote,
					From:       from,
					To:         to,
				}
				var stream io.Reader
				if err = remote.Call("IncrementalTransferRequest", &r, &stream); err != nil {
					log("error requesting incremental snapshot stream: %s", err)
					return false
				}

				log("invoking zfs receive")
				watcher := util.IOProgressWatcher{Reader: stream}
				watcher.KickOff(1*time.Second, func(p util.IOProgress) {
					log("progress on receive operation: %v bytes received", p.TotalRX)
				})

				if err = zfs.ZFSRecv(m.Local, &watcher); err != nil {
					log("error receiving stream: %s", err)
					return false
				}

				totalRx := watcher.Progress().TotalRX
				pathRx += totalRx
				log("finished incremental transfer, %v bytes total", totalRx)

			}

			log("finished following incremental path, %v bytes total", pathRx)
			return true

		case zfs.ConflictNoCommonAncestor:

			log("remote and local filesystem have snapshots, but no common one")
			log("perform manual replication to establish a common snapshot history")
			log("remote versions:")
			for _, v := range diff.MRCAPathRight {
				log(" %s (GUID %v)", v, v.Guid)
			}
			log("local versions:")
			for _, v := range diff.MRCAPathLeft {
				log(" %s (GUID %v)", v, v.Guid)
			}
			return false

		case zfs.ConflictDiverged:

			log("remote and local filesystem share a history but have diverged")
			log("perform manual replication or delete snapshots on the receiving" +
				"side  to establish an incremental replication parse")
			log("remote-only versions:")
			for _, v := range diff.MRCAPathRight {
				log(" %s (GUID %v)", v, v.Guid)
			}
			log("local-only versions:")
			for _, v := range diff.MRCAPathLeft {
				log(" %s (GUID %v)", v, v.Guid)
			}
			return false

		}

		panic("implementation error: this should not be reached")
		return false

	})

	return

}
