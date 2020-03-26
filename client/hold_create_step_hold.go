package client

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"github.com/spf13/pflag"

	"github.com/zrepl/zrepl/cli"
	"github.com/zrepl/zrepl/endpoint"
	"github.com/zrepl/zrepl/zfs"
)

var holdsCreateStepHoldFlags struct {
	target string
	jobid  JobIDFlag
}

var holdsCmdCreateStepHold = &cli.Subcommand{
	Use:             "step",
	Run:             doHoldsCreateStep,
	NoRequireConfig: true,
	Short:           `create a step hold or bookmark`,
	SetupFlags: func(f *pflag.FlagSet) {
		f.StringVarP(&holdsCreateStepHoldFlags.target, "target", "t", "", "snapshot to be held / bookmark to be held")
		f.VarP(&holdsCreateStepHoldFlags.jobid, "jobid", "j", "jobid for which the hold is installed")
	},
}

func doHoldsCreateStep(sc *cli.Subcommand, args []string) error {
	if len(args) > 0 {
		return errors.New("subcommand takes no arguments")
	}

	f := &holdsCreateStepHoldFlags

	fs, _, _, err := zfs.DecomposeVersionString(f.target)
	if err != nil {
		return errors.Wrapf(err, "%q invalid target", f.target)
	}

	if f.jobid.FlagValue() == nil {
		return errors.Errorf("jobid must be set")
	}

	ctx := context.Background()

	v, err := zfs.ZFSGetFilesystemVersion(ctx, f.target)
	if err != nil {
		return errors.Wrapf(err, "get info about target %q", f.target)
	}

	step, err := endpoint.HoldStep(ctx, fs, v, *f.jobid.FlagValue())
	if err != nil {
		return errors.Wrap(err, "create step hold")
	}
	fmt.Println(step.String())
	return nil
}
