package client

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/cli"
	"github.com/zrepl/zrepl/client/status/client"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/job"
	"github.com/zrepl/zrepl/daemon/pruner"
	"github.com/zrepl/zrepl/daemon/snapper"
	"github.com/zrepl/zrepl/replication/report"
)

var WaitJobCmd = &cli.Subcommand{
	Use:   "wait-job JOB",
	Short: "block and wait until the specified job is done or has failed",
	Run: func(ctx context.Context, subcommand *cli.Subcommand, args []string) error {
		return runWaitJobCmd(subcommand.Config(), args)
	},
}

// adapted from status.go
func runWaitJobCmd(config *config.Config, args []string) error {
	if len(args) != 1 {
		return errors.Errorf("Expected 1 argument: JOB")
	}

	// TODO: poll status until done or error
	c, err := client.New("unix", config.Global.Control.SockPath)
	if err != nil {
		return errors.Wrapf(err, "connect to daemon socket at %q", config.Global.Control.SockPath)
	}

	jobName := args[0]

	updateAndEvaluate := func() (bool, error) {
		s, err := c.Status()
		if err != nil {
			return true, err
		}

		if err != nil {
			return true, fmt.Errorf("can't get status from daemon: %w", err)
		}

		job, ok := s.Jobs[jobName]
		if !ok {
			return true, fmt.Errorf("job not in status: %v", jobName)
		}

		return evaluateJobStatus(job)
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

func evaluateJobStatus(j *job.Status) (bool, error) {

	if j.Type == job.TypePush || j.Type == job.TypePull {
		activeStatus, ok := j.JobSpecific.(*job.ActiveSideStatus)
		if !ok || activeStatus == nil {
			// TODO: ignore?
			return true, nil //error.New("ActiveSideStatus is null")
		}

		//t.renderReplicationReport(activeStatus.Replication, t.getReplicationProgressHistory(k))
		replicationDone, err := evaluateReplicationStatus(activeStatus.Replication)
		if err != nil {
			return true, err
		}

		senderPruningDone, err := evaluatePruningStatus(activeStatus.PruningSender)
		if err != nil {
			return true, fmt.Errorf("sender %w", err)
		}
		receiverPruningDone, err := evaluatePruningStatus(activeStatus.PruningReceiver)
		if err != nil {
			return true, fmt.Errorf("receiver %w", err)
		}

		done := replicationDone && senderPruningDone && receiverPruningDone

		if j.Type == job.TypePush {
			snapperDone, err := evaluateSnapperStatus(activeStatus.Snapshotting)
			if err != nil {
				return true, err
			}

			done = done && snapperDone
		}

		return done, nil
	} else if j.Type == job.TypeSnap {
		snapStatus, ok := j.JobSpecific.(*job.SnapJobStatus)
		if !ok || snapStatus == nil {
			// TODO: ignore?
			return true, nil //error.Error("SnapJobStatus is null")
		}

		pruningDone, err := evaluatePruningStatus(snapStatus.Pruning)
		if err != nil {
			return true, err
		}

		snapperDone, err := evaluateSnapperStatus(snapStatus.Snapshotting)
		if err != nil {
			return true, err
		}

		return pruningDone && snapperDone, nil
	} else if j.Type == job.TypeSource /*|| j.Type == job.TypeSink*/ {
		snapStatus, ok := j.JobSpecific.(*job.PassiveStatus)
		if !ok || snapStatus == nil {
			// TODO: ignore?
			return true, nil //error.Error("SnapJobStatus is null")
		}

		snapperDone, err := evaluateSnapperStatus(snapStatus.Snapper)
		if err != nil {
			return true, err
		}

		return snapperDone, nil

	} else {
		return true, fmt.Errorf("unknown job type")
	}
}

func evaluatePruningStatus(r *pruner.Report) (bool, error) {
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

	if prunerState.IsError() {
		return true, fmt.Errorf("pruning failed with state: %v", prunerState)
	}
	if prunerState.IsTerminal() {
		return true, nil
	}

	// no errros, but still not finished
	return false, nil
}

func evaluateReplicationStatus(rep *report.Report) (bool, error) {
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
	if latest.State.IsError() {
		return true, fmt.Errorf("last replication attept failed with state %v", latest.State)
	}
	if latest.State.IsTerminal() {
		return true, nil
	}

	// no errros, but still not finished
	return false, nil
}

func evaluateSnapperStatus(r *snapper.Report) (bool, error) {
	if r == nil {
		// ignore
		return true, nil
	}

	if r.Error != "" {
		return true, fmt.Errorf("snapshotting error: %v", r.Error)
	}

	if r.State.IsError() {
		// some filesystems had an error
		return true, fmt.Errorf("snapshotting failed with state: %v", r.State)
	}
	if r.State.IsTerminal() {
		return true, nil
	}

	// no errros, but still not finished
	return false, nil
}
