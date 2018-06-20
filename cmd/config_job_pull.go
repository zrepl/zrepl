package cmd

import (
	"time"

	"context"
	"fmt"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/util"
	"github.com/zrepl/zrepl/cmd/replication"
	"github.com/problame/go-streamrpc"
)

type PullJob struct {
	Name     string
	Connect  RWCConnecter
	Interval time.Duration
	Mapping  *DatasetMapFilter
	// constructed from mapping during parsing
	pruneFilter       *DatasetMapFilter
	SnapshotPrefix    string
	InitialReplPolicy InitialReplPolicy
	Prune             PrunePolicy
	Debug             JobDebugSettings

	task *Task
}

func parsePullJob(c JobParsingContext, name string, i map[string]interface{}) (j *PullJob, err error) {

	var asMap struct {
		Connect           map[string]interface{}
		Interval          string
		Mapping           map[string]string
		InitialReplPolicy string `mapstructure:"initial_repl_policy"`
		Prune             map[string]interface{}
		SnapshotPrefix    string `mapstructure:"snapshot_prefix"`
		Debug             map[string]interface{}
	}

	if err = mapstructure.Decode(i, &asMap); err != nil {
		err = errors.Wrap(err, "mapstructure error")
		return nil, err
	}

	j = &PullJob{Name: name}

	j.Connect, err = parseSSHStdinserverConnecter(asMap.Connect)
	if err != nil {
		err = errors.Wrap(err, "cannot parse 'connect'")
		return nil, err
	}

	if j.Interval, err = parsePostitiveDuration(asMap.Interval); err != nil {
		err = errors.Wrap(err, "cannot parse 'interval'")
		return nil, err
	}

	j.Mapping, err = parseDatasetMapFilter(asMap.Mapping, false)
	if err != nil {
		err = errors.Wrap(err, "cannot parse 'mapping'")
		return nil, err
	}

	if j.pruneFilter, err = j.Mapping.InvertedFilter(); err != nil {
		err = errors.Wrap(err, "cannot automatically invert 'mapping' for prune job")
		return nil, err
	}

	j.InitialReplPolicy, err = parseInitialReplPolicy(asMap.InitialReplPolicy, DEFAULT_INITIAL_REPL_POLICY)
	if err != nil {
		err = errors.Wrap(err, "cannot parse 'initial_repl_policy'")
		return
	}

	if j.SnapshotPrefix, err = parseSnapshotPrefix(asMap.SnapshotPrefix); err != nil {
		return
	}

	if j.Prune, err = parsePrunePolicy(asMap.Prune, false); err != nil {
		err = errors.Wrap(err, "cannot parse prune policy")
		return
	}

	if err = mapstructure.Decode(asMap.Debug, &j.Debug); err != nil {
		err = errors.Wrap(err, "cannot parse 'debug'")
		return
	}

	return
}

func (j *PullJob) JobName() string {
	return j.Name
}

func (j *PullJob) JobType() JobType { return JobTypePull }

func (j *PullJob) JobStart(ctx context.Context) {

	log := ctx.Value(contextKeyLog).(Logger)
	defer log.Info("exiting")
	j.task = NewTask("main", j, log)

	// j.task is idle here idle here

	ticker := time.NewTicker(j.Interval)
	for {
		j.doRun(ctx)
		select {
		case <-ctx.Done():
			j.task.Log().WithError(ctx.Err()).Info("context")
			return
		case <-ticker.C:
		}
	}
}

var STREAMRPC_CONFIG = &streamrpc.ConnConfig{ // FIXME oversight and configurability
	RxHeaderMaxLen: 4096,
	RxStructuredMaxLen: 4096 * 4096,
	RxStreamMaxChunkSize: 4096 * 4096,
	TxChunkSize: 4096 * 4096,
}

func (j *PullJob) doRun(ctx context.Context) {

	j.task.Enter("run")
	defer j.task.Finish()

	j.task.Log().Info("connecting")
	rwc, err := j.Connect.Connect()
	if err != nil {
		j.task.Log().WithError(err).Error("error connecting")
		return
	}

	rwc, err = util.NewReadWriteCloserLogger(rwc, j.Debug.Conn.ReadDump, j.Debug.Conn.WriteDump)
	if err != nil {
		return
	}


	client := RemoteEndpoint{streamrpc.NewClientOnConn(rwc, STREAMRPC_CONFIG)}
	if j.Debug.RPC.Log {
		// FIXME implement support
		// client.SetLogger(j.task.Log(), true)
	}

	j.task.Enter("pull")

	puller, err := NewReceiverEndpoint(
		j.Mapping,
		NewPrefixFilter(j.SnapshotPrefix),
	)
	if err != nil {
		j.task.Log().WithError(err).Error("error creating receiver endpoint")
		j.task.Finish()
		return
	}

	replicator := replication.NewIncrementalPathReplicator()
	replication.Replicate(context.WithValue(ctx, replication.ContextKeyLog, j.task.Log()), replication.NewEndpointPairPull(client, puller), replicator)

	closeRPCWithTimeout(j.task, client, time.Second*1, "")
	rwc.Close()
	j.task.Finish()

	j.task.Enter("prune")
	pruner, err := j.Pruner(j.task, PrunePolicySideDefault, false)
	if err != nil {
		j.task.Log().WithError(err).Error("error creating pruner")
	} else {
		pruner.Run(ctx)
	}
	j.task.Finish()

}

func (j *PullJob) JobStatus(ctxt context.Context) (*JobStatus, error) {
	return &JobStatus{Tasks: []*TaskStatus{j.task.Status()}}, nil
}

func (j *PullJob) Pruner(task *Task, side PrunePolicySide, dryRun bool) (p Pruner, err error) {
	p = Pruner{
		task,
		time.Now(),
		dryRun,
		j.pruneFilter,
		j.SnapshotPrefix,
		j.Prune,
	}
	return
}

func closeRPCWithTimeout(task *Task, remote RemoteEndpoint, timeout time.Duration, goodbye string) {

	task.Log().Info("closing rpc connection")

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
		task.Log().WithError(err).Error("error closing connection")
	}
	return
}
