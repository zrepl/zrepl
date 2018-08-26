package cmd

import (
	"context"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/problame/go-streamrpc"
	"github.com/zrepl/zrepl/cmd/endpoint"
	"net"
)

type SourceJob struct {
	Name           string
	Serve          ListenerFactory
	Filesystems    *DatasetMapFilter
	SnapshotPrefix string
	Interval       time.Duration
	Prune          PrunePolicy
	Debug          JobDebugSettings
}

func parseSourceJob(c JobParsingContext, name string, i map[string]interface{}) (j *SourceJob, err error) {

	var asMap struct {
		Serve          map[string]interface{}
		Filesystems    map[string]string
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

	if j.Filesystems, err = parseDatasetMapFilter(asMap.Filesystems, true); err != nil {
		return
	}

	if j.SnapshotPrefix, err = parseSnapshotPrefix(asMap.SnapshotPrefix); err != nil {
		return
	}

	if j.Interval, err = parsePostitiveDuration(asMap.Interval); err != nil {
		err = errors.Wrap(err, "cannot parse 'interval'")
		return
	}

	if j.Prune, err = parsePrunePolicy(asMap.Prune, true); err != nil {
		err = errors.Wrap(err, "cannot parse 'prune'")
		return
	}

	if err = mapstructure.Decode(asMap.Debug, &j.Debug); err != nil {
		err = errors.Wrap(err, "cannot parse 'debug'")
		return
	}

	if j.Debug.Conn.ReadDump != "" || j.Debug.Conn.WriteDump != "" {
		logServe := logListenerFactory{
			ListenerFactory: j.Serve,
			ReadDump:        j.Debug.Conn.ReadDump,
			WriteDump:       j.Debug.Conn.WriteDump,
		}
		j.Serve = logServe
	}

	return
}

func (j *SourceJob) JobName() string {
	return j.Name
}

func (j *SourceJob) JobType() JobType { return JobTypeSource }

func (j *SourceJob) JobStart(ctx context.Context) {

	log := getLogger(ctx)
	defer log.Info("exiting")

	a := IntervalAutosnap{j.Filesystems, j.SnapshotPrefix, j.Interval}
	p, err := j.Pruner(PrunePolicySideDefault, false)

	if err != nil {
		log.WithError(err).Error("error creating pruner")
		return
	}

	didSnaps := make(chan struct{})

	go j.serve(ctx) // logSubsysField set by handleConnection
	go a.Run(WithLogger(ctx, log.WithField(logSubsysField, "snap")), didSnaps)

outer:
	for {
		select {
		case <-ctx.Done():
			break outer
		case <-didSnaps:
			p.Run(WithLogger(ctx, log.WithField(logSubsysField, "prune")))
		}
	}
	log.WithError(ctx.Err()).Info("context")

}

func (j *SourceJob) Pruner(side PrunePolicySide, dryRun bool) (p Pruner, err error) {
	p = Pruner{
		time.Now(),
		dryRun,
		j.Filesystems,
		j.SnapshotPrefix,
		j.Prune,
	}
	return
}

func (j *SourceJob) serve(ctx context.Context) {

	log := getLogger(ctx)

	listener, err := j.Serve.Listen()
	if err != nil {
		getLogger(ctx).WithError(err).Error("error listening")
		return
	}

	type connChanMsg struct {
		conn net.Conn
		err  error
	}
	connChan := make(chan connChanMsg, 1)

	// Serve connections until interrupted or error
outer:
	for {

		go func() {
			rwc, err := listener.Accept()
			connChan <- connChanMsg{rwc, err}
		}()

		select {

		case rwcMsg := <-connChan:

			if rwcMsg.err != nil {
				log.WithError(rwcMsg.err).Error("error accepting connection")
				continue
			}

			j.handleConnection(ctx, rwcMsg.conn)

		case <-ctx.Done():
			log.WithError(ctx.Err()).Info("context")
			break outer
		}

	}

	log.Info("closing listener")
	err = listener.Close()
	if err != nil {
		log.WithError(err).Error("error closing listener")
	}

	return
}

func (j *SourceJob) handleConnection(ctx context.Context, conn net.Conn) {
	log := getLogger(ctx)
	log.Info("handling client connection")

	senderEP := endpoint.NewSender(j.Filesystems, NewPrefixFilter(j.SnapshotPrefix))

	ctx = endpoint.WithLogger(ctx, log.WithField(logSubsysField, "serve"))
	ctx = streamrpc.ContextWithLogger(ctx, streamrpcLogAdaptor{log.WithField(logSubsysField, "rpc")})
	handler := endpoint.NewHandler(senderEP)
	if err := streamrpc.ServeConn(ctx, conn, STREAMRPC_CONFIG, handler.Handle); err != nil {
		log.WithError(err).Error("error serving connection")
	} else {
		log.Info("client closed connection")
	}
}
