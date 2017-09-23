package cmd

import (
	"context"
	mapstructure "github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/rpc"
	"github.com/zrepl/zrepl/util"
	"io"
	"time"
)

type SourceJob struct {
	Name           string
	Serve          AuthenticatedChannelListenerFactory
	Datasets       *DatasetMapFilter
	SnapshotPrefix string
	Interval       time.Duration
	Prune          PrunePolicy
	Debug          JobDebugSettings
}

func parseSourceJob(c JobParsingContext, name string, i map[string]interface{}) (j *SourceJob, err error) {

	var asMap struct {
		Serve          map[string]interface{}
		Datasets       map[string]string
		SnapshotPrefix string `mapstructure:"snapshot_prefix"`
		Interval       string
		Prune          map[string]interface{}
		Debug          map[string]interface{}
	}

	if err = mapstructure.Decode(i, &asMap); err != nil {
		err = errors.Wrap(err, "mapstructure error")
		return nil, err
	}

	j = &SourceJob{Name: name}

	if j.Serve, err = parseAuthenticatedChannelListenerFactory(c, asMap.Serve); err != nil {
		return
	}

	if j.Datasets, err = parseDatasetMapFilter(asMap.Datasets, true); err != nil {
		return
	}

	if j.SnapshotPrefix, err = parseSnapshotPrefix(asMap.SnapshotPrefix); err != nil {
		return
	}

	if j.Interval, err = time.ParseDuration(asMap.Interval); err != nil {
		err = errors.Wrap(err, "cannot parse 'interval'")
		return
	}

	if j.Prune, err = parsePrunePolicy(asMap.Prune); err != nil {
		err = errors.Wrap(err, "cannot parse 'prune'")
		return
	}

	if err = mapstructure.Decode(asMap.Debug, &j.Debug); err != nil {
		err = errors.Wrap(err, "cannot parse 'debug'")
		return
	}

	return
}

func (j *SourceJob) JobName() string {
	return j.Name
}

func (j *SourceJob) JobStart(ctx context.Context) {

	log := ctx.Value(contextKeyLog).(Logger)
	defer log.Printf("exiting")

	a := IntervalAutosnap{DatasetFilter: j.Datasets, Prefix: j.SnapshotPrefix, SnapshotInterval: j.Interval}
	p, err := j.Pruner(PrunePolicySideDefault, false)
	if err != nil {
		log.Printf("error creating pruner: %s", err)
		return
	}

	snapContext := context.WithValue(ctx, contextKeyLog, log.WithField(logTaskField, "autosnap"))
	prunerContext := context.WithValue(ctx, contextKeyLog, log.WithField(logTaskField, "prune"))
	serveContext := context.WithValue(ctx, contextKeyLog, log.WithField(logTaskField, "serve"))
	didSnaps := make(chan struct{})

	go j.serve(serveContext)
	go a.Run(snapContext, didSnaps)

outer:
	for {
		select {
		case <-ctx.Done():
			break outer
		case <-didSnaps:
			log.Printf("starting pruner")
			p.Run(prunerContext)
			log.Printf("pruner done")
		}
	}
	log.Printf("context: %s", prunerContext.Err())

}

func (j *SourceJob) Pruner(side PrunePolicySide, dryRun bool) (p Pruner, err error) {
	p = Pruner{
		time.Now(),
		dryRun,
		j.Datasets,
		j.SnapshotPrefix,
		j.Prune,
	}
	return
}

func (j *SourceJob) serve(ctx context.Context) {

	log := ctx.Value(contextKeyLog).(Logger)

	listener, err := j.Serve.Listen()
	if err != nil {
		log.Printf("error listening: %s", err)
		return
	}

	rwcChan := make(chan io.ReadWriteCloser)

	// Serve connections until interrupted or error
outer:
	for {

		go func() {
			rwc, err := listener.Accept()
			if err != nil {
				log.Printf("error accepting connection: %s", err)
				close(rwcChan)
				return
			}
			rwcChan <- rwc
		}()

		select {

		case rwc, notClosed := <-rwcChan:

			if !notClosed {
				break outer // closed because of accept error
			}

			rwc, err := util.NewReadWriteCloserLogger(rwc, j.Debug.Conn.ReadDump, j.Debug.Conn.WriteDump)
			if err != nil {
				panic(err)
			}

			// construct connection handler
			handler := NewHandler(log, j.Datasets, &PrefixSnapshotFilter{j.SnapshotPrefix})

			// handle connection
			rpcServer := rpc.NewServer(rwc)
			if j.Debug.RPC.Log {
				rpclog := log.WithField("subsystem", "rpc")
				rpcServer.SetLogger(rpclog, true)
			}
			registerEndpoints(rpcServer, handler)
			if err = rpcServer.Serve(); err != nil {
				log.Printf("error serving connection: %s", err)
			}
			rwc.Close()

		case <-ctx.Done():
			log.Printf("context: %s", ctx.Err())
			break outer

		}

	}

	log.Printf("closing listener")
	err = listener.Close()
	if err != nil {
		log.Printf("error closing listener: %s", err)
	}

	return

}
