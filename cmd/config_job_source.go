package cmd

import (
	"context"
	"io"
	"time"

	mapstructure "github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/rpc"
	"github.com/zrepl/zrepl/util"
)

type SourceJob struct {
	Name           string
	Serve          AuthenticatedChannelListenerFactory
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

	return
}

func (j *SourceJob) JobName() string {
	return j.Name
}

func (j *SourceJob) JobStart(ctx context.Context) {

	log := ctx.Value(contextKeyLog).(Logger)
	defer log.Info("exiting")

	j.autosnapTask = NewTask("autosnap", log)
	j.pruneTask = NewTask("prune", log)
	j.serveTask = NewTask("serve", log)

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

	listener, err := j.Serve.Listen()
	if err != nil {
		task.Log().WithError(err).Error("error listening")
		return
	}

	type rwcChanMsg struct {
		rwc io.ReadWriteCloser
		err error
	}
	rwcChan := make(chan rwcChanMsg)

	// Serve connections until interrupted or error
outer:
	for {

		go func() {
			rwc, err := listener.Accept()
			if err != nil {
				rwcChan <- rwcChanMsg{rwc, err}
				close(rwcChan)
				return
			}
			rwcChan <- rwcChanMsg{rwc, err}
		}()

		select {

		case rwcMsg := <-rwcChan:

			if rwcMsg.err != nil {
				task.Log().WithError(err).Error("error accepting connection")
				break outer
			}

			j.handleConnection(rwcMsg.rwc, task)

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

func (j *SourceJob) handleConnection(rwc io.ReadWriteCloser, task *Task) {

	task.Enter("handle_connection")
	defer task.Finish()

	task.Log().Info("handling client connection")

	rwc, err := util.NewReadWriteCloserLogger(rwc, j.Debug.Conn.ReadDump, j.Debug.Conn.WriteDump)
	if err != nil {
		panic(err)
	}

	// construct connection handler
	handler := NewHandler(task.Log(), j.Filesystems, NewPrefixFilter(j.SnapshotPrefix))

	// handle connection
	rpcServer := rpc.NewServer(rwc)
	if j.Debug.RPC.Log {
		rpclog := task.Log().WithField("subsystem", "rpc")
		rpcServer.SetLogger(rpclog, true)
	}
	registerEndpoints(rpcServer, handler)
	if err = rpcServer.Serve(); err != nil {
		task.Log().WithError(err).Error("error serving connection")
	}

	// wait for client to close connection
	// FIXME: we cannot just close it like we would to with a TCP socket because
	// FIXME: go-nettsh's Close() may overtake the remaining data in the pipe
	const CLIENT_HANGUP_TIMEOUT = 1 * time.Second
	task.Log().
		WithField("timeout", CLIENT_HANGUP_TIMEOUT).
		Debug("waiting for client to hang up")

	wchan := make(chan error)
	go func() {
		var pseudo [1]byte
		_, err := io.ReadFull(rwc, pseudo[:])
		wchan <- err
	}()
	var werr error
	select {
	case werr = <-wchan:
		// all right
	case <-time.After(CLIENT_HANGUP_TIMEOUT):
		werr = errors.New("client did not close connection within timeout")
	}
	if werr != nil && werr != io.EOF {
		task.Log().WithError(werr).
			Error("error waiting for client to hang up")
	}
	task.Log().Info("closing client connection")
	if err = rwc.Close(); err != nil {
		task.Log().WithError(err).Error("error force-closing connection")
	}
}
