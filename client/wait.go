package client

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/cli"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon"
	"github.com/zrepl/zrepl/daemon/job"
	"github.com/zrepl/zrepl/daemon/pruner"
	"github.com/zrepl/zrepl/daemon/snapper"
	"github.com/zrepl/zrepl/replication/report"
)

type wui struct {
	//lock      sync.Mutex //For report and error
	report map[string]*job.Status

	jobName string
}

var WaitCmd = &cli.Subcommand{
	Use:   "wait JOB",
	Short: "block and wait until the specified job is done or has failed",
	Run: func(ctx context.Context, subcommand *cli.Subcommand, args []string) error {
		return runWaitCmd(subcommand.Config(), args)
	},
}

// adapted from status.go
func runWaitCmd(config *config.Config, args []string) error {
	if len(args) != 1 {
		return errors.Errorf("Expected 1 argument: JOB")
	}

	// TODO: poll status until done or error
	httpc, err := controlHttpClient(config.Global.Control.SockPath)
	if err != nil {
		return err
	}

	t := wui{
		report:  nil,
		jobName: args[0],
	}

	updateAndEvaluate := func() (bool, error) {
		var m daemon.Status

		err := jsonRequestResponse(httpc, daemon.ControlJobEndpointStatus,
			struct{}{},
			&m,
		)
		if err != nil {
			return true, fmt.Errorf("can't get status from daemon: %w", err)
		}

		t.report = m.Jobs

		return t.evaluate()
	}
	updateAndEvaluate()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
ticker:
	for range ticker.C {
		done, err := updateAndEvaluate()
		if err != nil {
			return err
		}
		if done {
			break ticker
		}
	}

	return nil
}

func (t *wui) evaluate() (bool, error) {
	v, ok := t.report[t.jobName]
	if !ok {
		return true, fmt.Errorf("job not in status: %v", t.jobName)
	}

	if v.Type == job.TypePush || v.Type == job.TypePull {
		activeStatus, ok := v.JobSpecific.(*job.ActiveSideStatus)
		if !ok || activeStatus == nil {
			// TODO: ignore?
			return true, nil //error.New("ActiveSideStatus is null")
		}

		//t.renderReplicationReport(activeStatus.Replication, t.getReplicationProgressHistory(k))
		replicationDone, err := evaluateReplicationReport(activeStatus.Replication)
		if err != nil {
			return true, err
		}

		senderPruningDone, err := evaluatePruningReport(activeStatus.PruningSender)
		if err != nil {
			return true, fmt.Errorf("sender %w", err)
		}
		receiverPruningDone, err := evaluatePruningReport(activeStatus.PruningReceiver)
		if err != nil {
			return true, fmt.Errorf("receiver %w", err)
		}

		done := replicationDone && senderPruningDone && receiverPruningDone

		if v.Type == job.TypePush {
			snapperDone, err := evaluateSnapperReport(activeStatus.Snapshotting)
			if err != nil {
				return true, err
			}

			done = done && snapperDone
		}

		return done, nil
	} else if v.Type == job.TypeSnap {
		snapStatus, ok := v.JobSpecific.(*job.SnapJobStatus)
		if !ok || snapStatus == nil {
			// TODO: ignore?
			return true, nil //error.Error("SnapJobStatus is null")
		}

		pruningDone, err := evaluatePruningReport(snapStatus.Pruning)
		if err != nil {
			return true, err
		}

		snapperDone, err := evaluateSnapperReport(snapStatus.Snapshotting)
		if err != nil {
			return true, err
		}

		return pruningDone && snapperDone, nil
	} else if v.Type == job.TypeSource || v.Type == job.TypeSink {
		snapStatus, ok := v.JobSpecific.(*job.PassiveStatus)
		if !ok || snapStatus == nil {
			// TODO: ignore?
			return true, nil //error.Error("SnapJobStatus is null")
		}

		snapperDone, err := evaluateSnapperReport(snapStatus.Snapper)
		if err != nil {
			return true, err
		}

		return snapperDone, nil

	} else {
		return true, fmt.Errorf("unknown job type")
	}
}

func evaluatePruningReport(r *pruner.Report) (bool, error) {
	if r == nil {
		// ignore
		return true, nil
	}

	if r.Error != "" {
		return true, fmt.Errorf("pruning error: %v", r.Error)
	}

	prunerState, err := pruner.StateString(r.State)
	if err != nil {
		return true, fmt.Errorf("parsing pruning state %v: %w", r.State, err)
	}

	switch prunerState {
	case pruner.PlanErr:
		fallthrough
	case pruner.ExecErr:
		return true, fmt.Errorf("pruning failed with state: %v", prunerState)
	case pruner.Done:
		return true, nil
	default:
		// no errros, but still not finished
		return false, nil
	}
}

func evaluateReplicationReport(rep *report.Report) (bool, error) {
	if rep == nil {
		// ignore
		return true, nil
	}

	if rep.WaitReconnectError != nil {
		return true, fmt.Errorf("Connectivity: %v", rep.WaitReconnectError)
	}
	if !rep.WaitReconnectSince.IsZero() {
		delta := time.Until(rep.WaitReconnectUntil).Round(time.Second)
		if rep.WaitReconnectUntil.IsZero() || delta > 0 {
			// ignore, will reconnect
		} else {
			return true, fmt.Errorf("Connectivity: reconnects reached hard-fail timeout @ %s", rep.WaitReconnectUntil)
		}
	}

	if len(rep.Attempts) == 0 {
		return false, nil
	}

	latest := rep.Attempts[len(rep.Attempts)-1]
	switch latest.State {
	case report.AttemptPlanningError:
		fallthrough
	case report.AttemptFanOutError:
		return true, fmt.Errorf("last replication attept failed with state %v", latest.State)
	case report.AttemptDone:
		return true, nil
	default:
		// no errros, but still not finished
		return false, nil
	}
}

func evaluateSnapperReport(r *snapper.Report) (bool, error) {
	if r == nil {
		// ignore
		return true, nil
	}

	if r.Error != "" {
		return true, fmt.Errorf("snapshotting error: %v", r.Error)
	}

	switch r.State {
	case snapper.SyncUpErrWait:
		fallthrough
	case snapper.ErrorWait:
		// some filesystems had an error
		return true, fmt.Errorf("snapshotting failed with state: %v", r.State)
	case snapper.Waiting:
		// TODO: also snapper.Stopped
		return true, nil
	default:
		// no errros, but still not finished
		return false, nil
	}
}
