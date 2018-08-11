package cmd

import (
	"context"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/problame/go-streamrpc"
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
	serveTask      *Task
	autosnapTask   *Task
	pruneTask      *Task
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

	log := ctx.Value(contextKeyLog).(Logger)
	defer log.Info("exiting")

	j.autosnapTask = NewTask("autosnap", j, log)
	j.pruneTask = NewTask("prune", j, log)
	j.serveTask = NewTask("serve", j, log)

	a := IntervalAutosnap{j.autosnapTask, j.Filesystems, j.SnapshotPrefix, j.Interval}
	p, err := j.Pruner(j.pruneTask, PrunePolicySideDefault, false)

	if err != nil {
		log.WithError(err).Error("error creating pruner")
		return
	}

	didSnaps := make(chan struct{})

	go j.serve(ctx, j.serveTask)
	go a.Run(ctx, didSnaps)

outer:
	for {
		select {
		case <-ctx.Done():
			break outer
		case <-didSnaps:
			log.Info("starting pruner")
			p.Run(ctx)
			log.Info("pruner done")
		}
	}
	log.WithError(ctx.Err()).Info("context")

}

func (j *SourceJob) JobStatus(ctxt context.Context) (*JobStatus, error) {
	return &JobStatus{
		Tasks: []*TaskStatus{
			j.autosnapTask.Status(),
			j.pruneTask.Status(),
			j.serveTask.Status(),
		}}, nil
}

func (j *SourceJob) Pruner(task *Task, side PrunePolicySide, dryRun bool) (p Pruner, err error) {
	p = Pruner{
		task,
		time.Now(),
		dryRun,
		j.Filesystems,
		j.SnapshotPrefix,
		j.Prune,
	}
	return
}

func (j *SourceJob) serve(ctx context.Context, task *Task) {

	//listener, err := j.Serve.Listen()

	listener, err := net.Listen("tcp", ":8888")
	if err != nil {
		task.Log().WithError(err).Error("error listening")
		return
	}

	type connChanMsg struct {
		conn net.Conn
		err  error
	}
	connChan := make(chan connChanMsg)

	// Serve connections until interrupted or error
outer:
	for {

		go func() {
			rwc, err := listener.Accept()
			if err != nil {
				connChan <- connChanMsg{rwc, err}
				close(connChan)
				return
			}
			connChan <- connChanMsg{rwc, err}
		}()

		select {

		case rwcMsg := <-connChan:

			if rwcMsg.err != nil {
				task.Log().WithError(err).Error("error accepting connection")
				break outer
			}

			j.handleConnection(rwcMsg.conn, task)

		case <-ctx.Done():
			task.Log().WithError(ctx.Err()).Info("context")
			break outer

		}

	}

	task.Enter("close_listener")
	defer task.Finish()
	err = listener.Close()
	if err != nil {
		task.Log().WithError(err).Error("error closing listener")
	}

	return

}

func (j *SourceJob) handleConnection(conn net.Conn, task *Task) {

	task.Enter("handle_connection")
	defer task.Finish()

	task.Log().Info("handling client connection")

	senderEP := NewSenderEndpoint(j.Filesystems, NewPrefixFilter(j.SnapshotPrefix))

	ctx := context.Background()
	ctx = context.WithValue(ctx, contextKeyLog, task.Log().WithField("subsystem", "rpc.endpoint"))
	ctx = streamrpc.ContextWithLogger(ctx, streamrpcLogAdaptor{task.Log().WithField("subsystem", "rpc.protocol")})
	handler := HandlerAdaptor{senderEP}
	if err := streamrpc.ServeConn(ctx, conn, STREAMRPC_CONFIG, handler.Handle); err != nil {
		task.Log().WithError(err).Error("error serving connection")
	} else {
		task.Log().Info("client closed connection")
	}

}
