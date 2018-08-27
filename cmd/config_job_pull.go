package cmd

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"context"

	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/problame/go-streamrpc"
	"github.com/zrepl/zrepl/cmd/config"
	"github.com/zrepl/zrepl/cmd/endpoint"
	"github.com/zrepl/zrepl/replication"
)

type PullJob struct {
	Name     string
	Connect  streamrpc.Connecter
	Interval time.Duration
	Mapping  *DatasetMapFilter
	// constructed from mapping during parsing
	pruneFilter *DatasetMapFilter
	Prune       PrunePolicy

	rep *replication.Replication
}

func parsePullJob(c config.Global, in config.PullJob) (j *PullJob, err error) {

	j = &PullJob{Name: in.Name}

	j.Connect, err = parseConnect(in.Replication.Connect)
	if err != nil {
		err = errors.Wrap(err, "cannot parse 'connect'")
		return nil, err
	}

	j.Interval = in.Replication.Interval

	j.Mapping = NewDatasetMapFilter(1, false)
	if err := j.Mapping.Add("<", in.Replication.RootDataset); err != nil {
		return nil, err
	}

	j.pruneFilter = NewDatasetMapFilter(1, true)
	if err := j.pruneFilter.Add(in.Replication.RootDataset, MapFilterResultOk); err != nil {
		return nil, err
	}

	if j.Prune, err = parsePrunePolicy(asMap.Prune, false); err != nil {
		err = errors.Wrap(err, "cannot parse prune policy")
		return
	}

	if in.Debug.Conn.ReadDump != "" || j.Debug.Conn.WriteDump != "" {
		logConnecter := logNetConnConnecter{
			Connecter: j.Connect,
			ReadDump:  in.Debug.Conn.ReadDump,
			WriteDump: in.Debug.Conn.WriteDump,
		}
		j.Connect = logConnecter
	}

	return
}

func (j *PullJob) JobName() string { return j.Name }

func (j *PullJob) JobStart(ctx context.Context) {

	log := getLogger(ctx)
	defer log.Info("exiting")

	// j.task is idle here idle here
	usr1 := make(chan os.Signal)
	signal.Notify(usr1, syscall.SIGUSR1)
	defer signal.Stop(usr1)

	ticker := time.NewTicker(j.Interval)
	for {
		begin := time.Now()
		j.doRun(ctx)
		duration := time.Now().Sub(begin)
		if duration > j.Interval {
			log.
				WithField("actual_duration", duration).
				WithField("configured_interval", j.Interval).
				Warn("pull run took longer than configured interval")
		}
		select {
		case <-ctx.Done():
			log.WithError(ctx.Err()).Info("context")
			return
		case <-ticker.C:
		case <-usr1:
		}
	}
}

var STREAMRPC_CONFIG = &streamrpc.ConnConfig{ // FIXME oversight and configurability
	RxHeaderMaxLen:       4096,
	RxStructuredMaxLen:   4096 * 4096,
	RxStreamMaxChunkSize: 4096 * 4096,
	TxChunkSize:          4096 * 4096,
	RxTimeout: streamrpc.Timeout{
		Progress: 10 * time.Second,
	},
	TxTimeout: streamrpc.Timeout{
		Progress: 10 * time.Second,
	},
}

func (j *PullJob) doRun(ctx context.Context) {

	log := getLogger(ctx)
	// FIXME
	clientConf := &streamrpc.ClientConfig{
		ConnConfig: STREAMRPC_CONFIG,
	}

	client, err := streamrpc.NewClient(j.Connect, clientConf)
	defer client.Close()

	sender := endpoint.NewRemote(client)

	receiver, err := endpoint.NewReceiver(j.Mapping, AnyFSVFilter{})
	if err != nil {
		log.WithError(err).Error("error creating receiver endpoint")
		return
	}

	{
		ctx := replication.WithLogger(ctx, replicationLogAdaptor{log.WithField(logSubsysField, "replication")})
		ctx = streamrpc.ContextWithLogger(ctx, streamrpcLogAdaptor{log.WithField(logSubsysField, "rpc")})
		ctx = endpoint.WithLogger(ctx, log.WithField(logSubsysField, "endpoint"))
		j.rep = replication.NewReplication()
		j.rep.Drive(ctx, sender, receiver)
	}

	client.Close()

	{
		ctx := WithLogger(ctx, log.WithField(logSubsysField, "prune"))
		pruner, err := j.Pruner(PrunePolicySideDefault, false)
		if err != nil {
			log.WithError(err).Error("error creating pruner")
		} else {
			pruner.Run(ctx)
		}
	}
}

func (j *PullJob) Report() *replication.Report {
	return j.rep.Report()
}

func (j *PullJob) Pruner(side PrunePolicySide, dryRun bool) (p Pruner, err error) {
	p = Pruner{
		time.Now(),
		dryRun,
		j.pruneFilter,
		j.Prune,
	}
	return
}
