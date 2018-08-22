package cmd

import (
	"time"

	"context"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/zfs"
	"sync"
	"github.com/zrepl/zrepl/replication"
	"github.com/zrepl/zrepl/cmd/endpoint"
)

type LocalJob struct {
	Name              string
	Mapping           *DatasetMapFilter
	SnapshotPrefix    string
	Interval          time.Duration
	InitialReplPolicy replication.InitialReplPolicy
	PruneLHS          PrunePolicy
	PruneRHS          PrunePolicy
	Debug             JobDebugSettings
	snapperTask       *Task
	mainTask          *Task
	handlerTask       *Task
	pruneRHSTask      *Task
	pruneLHSTask      *Task
}

func parseLocalJob(c JobParsingContext, name string, i map[string]interface{}) (j *LocalJob, err error) {

	var asMap struct {
		Mapping           map[string]string
		SnapshotPrefix    string `mapstructure:"snapshot_prefix"`
		Interval          string
		InitialReplPolicy string                 `mapstructure:"initial_repl_policy"`
		PruneLHS          map[string]interface{} `mapstructure:"prune_lhs"`
		PruneRHS          map[string]interface{} `mapstructure:"prune_rhs"`
		Debug             map[string]interface{}
	}

	if err = mapstructure.Decode(i, &asMap); err != nil {
		err = errors.Wrap(err, "mapstructure error")
		return nil, err
	}

	j = &LocalJob{Name: name}

	if j.Mapping, err = parseDatasetMapFilter(asMap.Mapping, false); err != nil {
		return
	}

	if j.SnapshotPrefix, err = parseSnapshotPrefix(asMap.SnapshotPrefix); err != nil {
		return
	}

	if j.Interval, err = parsePostitiveDuration(asMap.Interval); err != nil {
		err = errors.Wrap(err, "cannot parse interval")
		return
	}

	if j.InitialReplPolicy, err = parseInitialReplPolicy(asMap.InitialReplPolicy, replication.DEFAULT_INITIAL_REPL_POLICY); err != nil {
		return
	}

	if j.PruneLHS, err = parsePrunePolicy(asMap.PruneLHS, true); err != nil {
		err = errors.Wrap(err, "cannot parse 'prune_lhs'")
		return
	}
	if j.PruneRHS, err = parsePrunePolicy(asMap.PruneRHS, false); err != nil {
		err = errors.Wrap(err, "cannot parse 'prune_rhs'")
		return
	}

	if err = mapstructure.Decode(asMap.Debug, &j.Debug); err != nil {
		err = errors.Wrap(err, "cannot parse 'debug'")
		return
	}

	return
}

func (j *LocalJob) JobName() string {
	return j.Name
}

func (j *LocalJob) JobType() JobType { return JobTypeLocal }

func (j *LocalJob) JobStart(ctx context.Context) {

	rootLog := ctx.Value(contextKeyLog).(Logger)

	j.snapperTask = NewTask("snapshot", j, rootLog)
	j.mainTask = NewTask("main", j, rootLog)
	j.handlerTask = NewTask("handler", j, rootLog)
	j.pruneRHSTask = NewTask("prune_rhs", j, rootLog)
	j.pruneLHSTask = NewTask("prune_lhs", j, rootLog)

	// Allow access to any dataset since we control what mapping
	// is passed to the pull routine.
	// All local datasets will be passed to its Map() function,
	// but only those for which a mapping exists will actually be pulled.
	// We can pay this small performance penalty for now.
	wildcardMapFilter := NewDatasetMapFilter(1, false)
	wildcardMapFilter.Add("<", "<")
	sender := &endpoint.Sender{wildcardMapFilter, NewPrefixFilter(j.SnapshotPrefix)}

	receiver, err := endpoint.NewReceiver(j.Mapping, NewPrefixFilter(j.SnapshotPrefix))
	if err != nil {
		rootLog.WithError(err).Error("unexpected error setting up local handler")
	}

	snapper := IntervalAutosnap{
		task:             j.snapperTask,
		DatasetFilter:    j.Mapping.AsFilter(),
		Prefix:           j.SnapshotPrefix,
		SnapshotInterval: j.Interval,
	}

	plhs, err := j.Pruner(j.pruneLHSTask, PrunePolicySideLeft, false)
	if err != nil {
		rootLog.WithError(err).Error("error creating lhs pruner")
		return
	}
	prhs, err := j.Pruner(j.pruneRHSTask, PrunePolicySideRight, false)
	if err != nil {
		rootLog.WithError(err).Error("error creating rhs pruner")
		return
	}

	didSnaps := make(chan struct{})
	go snapper.Run(ctx, didSnaps)

outer:
	for {

		select {
		case <-ctx.Done():
			j.mainTask.Log().WithError(ctx.Err()).Info("context")
			break outer
		case <-didSnaps:
			j.mainTask.Log().Debug("finished taking snapshots")
			j.mainTask.Log().Info("starting replication procedure")
		}

		j.mainTask.Log().Debug("replicating from lhs to rhs")
		j.mainTask.Enter("replicate")

		rep := replication.NewReplication()
		rep.Drive(ctx, sender, receiver)

		j.mainTask.Finish()

		// use a ctx as soon as Pull gains ctx support
		select {
		case <-ctx.Done():
			break outer
		default:
		}

		var wg sync.WaitGroup

		j.mainTask.Log().Info("pruning lhs")
		wg.Add(1)
		go func() {
			plhs.Run(ctx)
			wg.Done()
		}()

		j.mainTask.Log().Info("pruning rhs")
		wg.Add(1)
		go func() {
			prhs.Run(ctx)
			wg.Done()
		}()

		wg.Wait()

	}

}

func (j *LocalJob) JobStatus(ctxt context.Context) (*JobStatus, error) {
	return &JobStatus{Tasks: []*TaskStatus{
		j.snapperTask.Status(),
		j.pruneLHSTask.Status(),
		j.pruneRHSTask.Status(),
		j.mainTask.Status(),
	}}, nil
}

func (j *LocalJob) Pruner(task *Task, side PrunePolicySide, dryRun bool) (p Pruner, err error) {

	var dsfilter zfs.DatasetFilter
	var pp PrunePolicy
	switch side {
	case PrunePolicySideLeft:
		pp = j.PruneLHS
		dsfilter = j.Mapping.AsFilter()
	case PrunePolicySideRight:
		pp = j.PruneRHS
		dsfilter, err = j.Mapping.InvertedFilter()
		if err != nil {
			err = errors.Wrap(err, "cannot invert mapping for prune_rhs")
			return
		}
	default:
		err = errors.Errorf("must be either left or right side")
		return
	}

	p = Pruner{
		task,
		time.Now(),
		dryRun,
		dsfilter,
		j.SnapshotPrefix,
		pp,
	}

	return
}
