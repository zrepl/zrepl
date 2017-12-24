package cmd

import (
	"time"

	"context"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/rpc"
	"github.com/zrepl/zrepl/util"
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

	if j.Prune, err = parsePrunePolicy(asMap.Prune); err != nil {
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

func (j *PullJob) JobStart(ctx context.Context) {

	log := ctx.Value(contextKeyLog).(Logger)
	defer log.Info("exiting")

	ticker := time.NewTicker(j.Interval)

start:

	log.Info("connecting")
	rwc, err := j.Connect.Connect()
	if err != nil {
		log.WithError(err).Error("error connecting")
		return
	}

	rwc, err = util.NewReadWriteCloserLogger(rwc, j.Debug.Conn.ReadDump, j.Debug.Conn.WriteDump)
	if err != nil {
		return
	}

	client := rpc.NewClient(rwc)
	if j.Debug.RPC.Log {
		client.SetLogger(log, true)
	}

	log.Info("starting pull")

	pullLog := log.WithField(logTaskField, "pull")
	puller := Puller{client, pullLog, j.Mapping, j.InitialReplPolicy}
	if err = puller.doPull(); err != nil {
		log.WithError(err).Error("error doing pull")
	}

	closeRPCWithTimeout(log, client, time.Second*10, "")

	log.Info("starting prune")
	prunectx := context.WithValue(ctx, contextKeyLog, log.WithField(logTaskField, "prune"))
	pruner, err := j.Pruner(PrunePolicySideDefault, false)
	if err != nil {
		log.WithError(err).Error("error creating pruner")
		return
	}

	pruner.Run(prunectx)
	log.Info("finish prune")

	log.Info("wait for next interval")
	select {
	case <-ctx.Done():
		log.WithError(ctx.Err()).Info("context")
		return
	case <-ticker.C:
		goto start
	}

}

func (j *PullJob) Pruner(side PrunePolicySide, dryRun bool) (p Pruner, err error) {
	p = Pruner{
		time.Now(),
		dryRun,
		j.pruneFilter,
		j.SnapshotPrefix,
		j.Prune,
	}
	return
}

func (j *PullJob) doRun(ctx context.Context) {

}
