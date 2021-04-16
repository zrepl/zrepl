package trigger_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zrepl/zrepl/daemon/job/trigger"
	"github.com/zrepl/zrepl/daemon/logging/trace"
	"github.com/zrepl/zrepl/replication"
	"github.com/zrepl/zrepl/replication/driver"
	"github.com/zrepl/zrepl/replication/logic"
)

func TestBasics(t *testing.T) {

	var wg sync.WaitGroup
	defer wg.Wait()

	tr := trigger.New()

	triggered := make(chan int)
	waitForTriggerError := make(chan error)
	waitForResetCallToBeMadeByMainGoroutine := make(chan struct{})
	postResetAssertionsDone := make(chan struct{})

	taskCtx := context.Background()
	taskCtx, cancelTaskCtx := context.WithCancel(taskCtx)

	wg.Add(1)
	go func() {
		defer wg.Done()

		taskCtx := context.WithValue(taskCtx, "mykey", "myvalue")

		triggers := 0

	outer:
		for {
			invocationCtx, err := tr.WaitForTrigger(taskCtx)
			if err != nil {
				waitForTriggerError <- err
				return
			}
			require.Equal(t, invocationCtx.Value("mykey"), "myvalue")

			triggers++
			triggered <- triggers

			switch triggers {
			case 1:
				continue outer
			case 2:
				<-waitForResetCallToBeMadeByMainGoroutine
				require.Equal(t, context.Canceled, invocationCtx.Err(), "Reset() cancels invocation context")
				require.Nil(t, taskCtx.Err(), "Reset() does not cancel task context")
				close(postResetAssertionsDone)
			}

		}

	}()

	t.Logf("trigger 1")
	_, err := tr.Trigger()
	require.NoError(t, err)
	v := <-triggered
	require.Equal(t, 1, v)

	t.Logf("trigger 2")
	triggerResponse, err := tr.Trigger()
	require.NoError(t, err)
	v = <-triggered
	require.Equal(t, 2, v)

	t.Logf("reset")
	resetResponse, err := tr.Reset(trigger.ResetRequest{InvocationId: triggerResponse.InvocationId})
	require.NoError(t, err)
	t.Logf("reset response: %#v", resetResponse)
	close(waitForResetCallToBeMadeByMainGoroutine)
	<-postResetAssertionsDone

	t.Logf("cancel the context")
	cancelTaskCtx()
	wfte := <-waitForTriggerError
	require.Equal(t, taskCtx.Err(), wfte)

}

type PushJob struct {
	snap *Snapshotter
	repl *ReplicationAndTriggerRemotePruningSequence
}

func (j *PushJob) Handle(w http.ResponseWriter, r *http.Request) {
	panic("unimplemented")
}

func (j *PushJob) HandleTrigger(w http.ResponseWriter, r *http.Request) {
	panic("unimplemented")
}

func (j *PushJob) Run(ctx context.Context) {

}

type SnapshotSequence struct {

}

type ReplicationAndTriggerRemotePruningSequence struct {

}

func (s ReplicationAndTriggerRemotePruningSequence) Run(ctx context.Context) {

	j.mode.ConnectEndpoints(ctx, j.connecter)
	defer j.mode.DisconnectEndpoints()

	sender, receiver := j.mode.SenderReceiver()

	{
		select {
		case <-ctx.Done():
			return
		default:
		}
		ctx, endSpan := trace.WithSpan(ctx, "replication")
		ctx, repCancel := context.WithCancel(ctx)
		var repWait driver.WaitFunc
		j.updateTasks(func(tasks *activeSideTasks) {
			// reset it
			*tasks = activeSideTasks{}
			tasks.replicationCancel = func() { repCancel(); endSpan() }
			tasks.replicationReport, repWait = replication.Do(
				ctx, j.replicationDriverConfig, logic.NewPlanner(j.promRepStateSecs, j.promBytesReplicated, sender, receiver, j.mode.PlannerPolicy()),
			)
			tasks.state = ActiveSideReplicating
		})
		GetLogger(ctx).Info("start replication")
		repWait(true) // wait blocking
		repCancel()   // always cancel to free up context resources
		replicationReport := j.tasks.replicationReport()
		j.promReplicationErrors.Set(float64(replicationReport.GetFailedFilesystemsCountInLatestAttempt()))
		j.updateTasks(func(tasks *activeSideTasks) {
			tasks.replicationDone = replicationReport
		})
		endSpan()
	}

	{
		select {
		case <-ctx.Done():
			return
		default:
		}
		ctx, endSpan := trace.WithSpan(ctx, "prune_sender")
		ctx, senderCancel := context.WithCancel(ctx)
		tasks := j.updateTasks(func(tasks *activeSideTasks) {
			tasks.prunerSender = j.prunerFactory.BuildSenderPruner(ctx, sender, sender)
			tasks.prunerSenderCancel = func() { senderCancel(); endSpan() }
			tasks.state = ActiveSidePruneSender
		})
		GetLogger(ctx).Info("start pruning sender")
		tasks.prunerSender.Prune()
		GetLogger(ctx).Info("finished pruning sender")
		senderCancel()
		endSpan()
	}
	{
		select {
		case <-ctx.Done():
			return
		default:
		}
		ctx, endSpan := trace.WithSpan(ctx, "prune_recever")
		ctx, receiverCancel := context.WithCancel(ctx)
		tasks := j.updateTasks(func(tasks *activeSideTasks) {
			tasks.prunerReceiver = j.prunerFactory.BuildReceiverPruner(ctx, receiver, sender)
			tasks.prunerReceiverCancel = func() { receiverCancel(); endSpan() }
			tasks.state = ActiveSidePruneReceiver
		})
		GetLogger(ctx).Info("start pruning receiver")
		tasks.prunerReceiver.Prune()
		GetLogger(ctx).Info("finished pruning receiver")
		receiverCancel()
		endSpan()
	}

	j.updateTasks(func(tasks *activeSideTasks) {
		tasks.state = ActiveSideDone
	})

}


func TestUseCase(t *testing.T) {

	var as ActiveSide

	as.
	
}
