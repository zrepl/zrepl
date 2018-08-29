package job

import (
	"context"
	"github.com/pkg/errors"
	"github.com/problame/go-streamrpc"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/connecter"
	"github.com/zrepl/zrepl/daemon/filters"
	"github.com/zrepl/zrepl/daemon/logging"
	"github.com/zrepl/zrepl/endpoint"
	"github.com/zrepl/zrepl/replication"
	"sync"
	"github.com/zrepl/zrepl/daemon/pruner"
	"github.com/zrepl/zrepl/pruning"
)

type Push struct {
	name      string
	connecter streamrpc.Connecter
	fsfilter  endpoint.FSFilter

	keepRulesSender []pruning.KeepRule
	keepRulesReceiver []pruning.KeepRule

	mtx         sync.Mutex
	replication *replication.Replication
}

func PushFromConfig(g config.Global, in *config.PushJob) (j *Push, err error) {

	j = &Push{}
	j.name = in.Name

	j.connecter, err = connecter.FromConfig(g, in.Replication.Connect)

	if j.fsfilter, err = filters.DatasetMapFilterFromConfig(in.Replication.Filesystems); err != nil {
		return nil, errors.Wrap(err, "cannnot build filesystem filter")
	}

	return j, nil
}

func (j *Push) Name() string { return j.name }

func (j *Push) Status() interface{} {
	return nil // FIXME
}

func (j *Push) Run(ctx context.Context) {
	log := GetLogger(ctx)

	defer log.Info("job exiting")

	log.Debug("wait for wakeups")

	invocationCount := 0
outer:
	for {
		select {
		case <-ctx.Done():
			log.WithError(ctx.Err()).Info("context")
			break outer
		case <-WaitWakeup(ctx):
			invocationCount++
			invLog := log.WithField("invocation", invocationCount)
			j.do(WithLogger(ctx, invLog))
		}
	}
}

func (j *Push) do(ctx context.Context) {

	log := GetLogger(ctx)

	client, err := streamrpc.NewClient(j.connecter, &streamrpc.ClientConfig{STREAMRPC_CONFIG})
	if err != nil {
		log.WithError(err).Error("cannot create streamrpc client")
	}
	defer client.Close()

	sender := endpoint.NewSender(j.fsfilter, filters.NewAnyFSVFilter())
	receiver := endpoint.NewRemote(client)

	j.mtx.Lock()
	rep := replication.NewReplication()
	j.mtx.Unlock()

	ctx = logging.WithSubsystemLoggers(ctx, log)
	rep.Drive(ctx, sender, receiver)

	// Prune sender
	senderPruner := pruner.NewPruner(sender, receiver, j.keepRulesSender)
	senderPruner.Prune(ctx)

	// Prune receiver
	receiverPruner := pruner.NewPruner(receiver, receiver, j.keepRulesReceiver)
	receiverPruner.Prune(ctx)

}

