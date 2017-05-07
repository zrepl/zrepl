package main

import (
	"fmt"
	"github.com/urfave/cli"
	"github.com/zrepl/zrepl/jobrun"
	"github.com/zrepl/zrepl/rpc"
	"github.com/zrepl/zrepl/sshbytestream"
	"github.com/zrepl/zrepl/zfs"
	"io"
	"log"
	"os"
	"runtime/debug"
	"sync"
	"time"
)

type Logger interface {
	Printf(format string, v ...interface{})
}

var conf Config
var runner *jobrun.JobRunner
var logFlags int = log.LUTC | log.Ldate | log.Ltime
var defaultLog Logger

func main() {

	defer func() {
		_ = recover()
		defaultLog.Printf("panic:\n%s", debug.Stack())
		os.Exit(1)
	}()

	app := cli.NewApp()

	app.Name = "zrepl"
	app.Usage = "replicate zfs datasets"
	app.EnableBashCompletion = true
	app.Flags = []cli.Flag{
		cli.StringFlag{Name: "config"},
	}
	app.Before = func(c *cli.Context) (err error) {

		defaultLog = log.New(os.Stderr, "", logFlags)

		if !c.GlobalIsSet("config") {
			return cli.NewExitError("config flag not set", 2)
		}
		if conf, err = ParseConfig(c.GlobalString("config")); err != nil {
			return cli.NewExitError(err, 2)
		}

		jobrunLogger := log.New(os.Stderr, "jobrun ", logFlags)
		runner = jobrun.NewJobRunner(jobrunLogger)
		return
	}
	app.Commands = []cli.Command{
		{
			Name:    "sink",
			Aliases: []string{"s"},
			Usage:   "start in sink mode",
			Flags: []cli.Flag{
				cli.StringFlag{Name: "identity"},
				cli.StringFlag{Name: "logfile"},
			},
			Action: doSink,
		},
		{
			Name:    "run",
			Aliases: []string{"r"},
			Usage:   "do replication",
			Action:  doRun,
		},
	}

	app.Run(os.Args)

}

func doSink(c *cli.Context) (err error) {

	if !c.IsSet("identity") {
		return cli.NewExitError("identity flag not set", 2)
	}
	identity := c.String("identity")

	var logOut io.Writer
	if c.IsSet("logfile") {
		logOut, err = os.OpenFile(c.String("logfile"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			return
		}
	} else {
		logOut = os.Stderr
	}

	var sshByteStream io.ReadWriteCloser
	if sshByteStream, err = sshbytestream.Incoming(); err != nil {
		return
	}

	findMapping := func(cm []ClientMapping) zfs.DatasetMapping {
		for i := range cm {
			if cm[i].From == identity {
				return cm[i].Mapping
			}
		}
		return nil
	}

	sinkLogger := log.New(logOut, fmt.Sprintf("sink[%s] ", identity), logFlags)
	handler := Handler{
		Logger:      sinkLogger,
		PushMapping: findMapping(conf.Sinks),
		PullMapping: findMapping(conf.PullACLs),
	}

	if err = rpc.ListenByteStreamRPC(sshByteStream, handler); err != nil {
		//os.Exit(1)
		err = cli.NewExitError(err, 1)
		defaultLog.Printf("listenbytestreamerror: %#v\n", err)
	}

	return

}

func doRun(c *cli.Context) error {

	// Do every pull, do every push
	// Scheduling

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runner.Start()
	}()

	for i := range conf.Pulls {
		pull := conf.Pulls[i]

		j := jobrun.Job{
			Name:     fmt.Sprintf("pull%d", i),
			Interval: time.Duration(5 * time.Second),
			Repeats:  true,
			RunFunc: func(log jobrun.Logger) error {
				log.Printf("doing pull: %v", pull)
				return doPull(pull, log)
			},
		}

		runner.AddJob(j)
	}

	for i := range conf.Pushs {
		push := conf.Pushs[i]

		j := jobrun.Job{
			Name:     fmt.Sprintf("push%d", i),
			Interval: time.Duration(5 * time.Second),
			Repeats:  true,
			RunFunc: func(log jobrun.Logger) error {
				log.Printf("%v: %#v\n", time.Now(), push)
				return nil
			},
		}

		runner.AddJob(j)
	}

	for {
		select {
		case job := <-runner.NotificationChan():
			log.Printf("notificaiton on job %s: error=%v\n", job.Name, job.LastError)
		}
	}

	wg.Wait()

	return nil
}

func doPull(pull Pull, log jobrun.Logger) (err error) {

	if lt, ok := pull.From.Transport.(LocalTransport); ok {
		lt.SetHandler(Handler{
			Logger:      log,
			PullMapping: pull.Mapping,
		})
		pull.From.Transport = lt
		log.Printf("fixing up local transport: %#v", pull.From.Transport)
	}

	var remote rpc.RPCRequester

	if remote, err = pull.From.Transport.Connect(); err != nil {
		return
	}

	fsr := rpc.FilesystemRequest{
		Direction: rpc.DirectionPull,
	}
	var remoteFilesystems []zfs.DatasetPath
	if remoteFilesystems, err = remote.FilesystemRequest(fsr); err != nil {
		return
	}

	type RemoteLocalMapping struct {
		Remote      zfs.DatasetPath
		Local       zfs.DatasetPath
		LocalExists bool
	}
	replMapping := make([]RemoteLocalMapping, 0, len(remoteFilesystems))

	{

		localExists, err := zfs.ZFSListFilesystemExists()
		if err != nil {
			log.Printf("cannot get local filesystems map: %s", err)
			return err
		}

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
			replMapping = append(replMapping, m)
		}

	}

	log.Printf("remoteFilesystems: %#v\nreplMapping: %#v\n", remoteFilesystems, replMapping)

	// per fs sync, assume sorted in top-down order TODO
replMappingLoop:
	for _, m := range replMapping {

		log := func(format string, args ...interface{}) {
			log.Printf("[%s => %s]: %s", m.Remote.ToString(), m.Local.ToString(), fmt.Sprintf(format, args...))
		}

		log("mapping: %#v\n", m)

		var versions []zfs.FilesystemVersion
		if m.LocalExists {
			if versions, err = zfs.ZFSListFilesystemVersions(m.Local); err != nil {
				log("cannot get filesystem versions, stopping...: %v\n", m.Local.ToString(), m, err)
				break replMappingLoop
			}
		}

		var theirVersions []zfs.FilesystemVersion
		theirVersions, err = remote.FilesystemVersionsRequest(rpc.FilesystemVersionsRequest{
			Filesystem: m.Remote,
		})
		if err != nil {
			log("cannot fetch remote filesystem versions, stopping: %s", err)
			break replMappingLoop
		}

		diff := zfs.MakeFilesystemDiff(versions, theirVersions)
		log("diff: %#v\n", diff)

		if diff.IncrementalPath == nil {
			log("performing initial sync, following policy: %#v", pull.InitialReplPolicy)

			if pull.InitialReplPolicy != InitialReplPolicyMostRecent {
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
				break replMappingLoop
			}

			r := rpc.InitialTransferRequest{
				Filesystem:        m.Remote,
				FilesystemVersion: snapsOnly[len(snapsOnly)-1],
			}

			log("requesting initial transfer")

			var stream io.Reader
			if stream, err = remote.InitialTransferRequest(r); err != nil {
				log("error initial transfer request, stopping...: %s", err)
				break replMappingLoop
			}

			log("received initial transfer request response. zfs recv...")

			if err = zfs.ZFSRecv(m.Local, stream, "-u"); err != nil {
				log("error receiving stream, stopping...: %s", err)
				break replMappingLoop
			}

			log("configuring properties of received filesystem")

			if err = zfs.ZFSSet(m.Local, "readonly", "on"); err != nil {

			}

			log("finished initial transfer")

		} else if len(diff.IncrementalPath) < 2 {
			log("remote and local are in sync")
		} else {

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
					break replMappingLoop
				}

				log("receving incremental transfer")

				if err = zfs.ZFSRecv(m.Local, stream); err != nil {
					log("error receiving stream, stopping...: %s", err)
					break replMappingLoop
				}

				log("finished incremental transfer")

			}

			log("finished incremental transfer path")

		}

	}
	return nil
}
