package client

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"github.com/spf13/pflag"

	"github.com/LyingCak3/zrepl/internal/cli"
	"github.com/LyingCak3/zrepl/internal/endpoint"
	"github.com/LyingCak3/zrepl/internal/zfs"
)

var zabsCreateStepHoldFlags struct {
	target string
	jobid  JobIDFlag
}

var zabsCmdCreateStepHold = &cli.Subcommand{
	Use:             "step",
	Run:             doZabsCreateStep,
	NoRequireConfig: true,
	Short:           `create a step hold or bookmark`,
	SetupFlags: func(f *pflag.FlagSet) {
		f.StringVarP(&zabsCreateStepHoldFlags.target, "target", "t", "", "snapshot to be held / bookmark to be held")
		f.VarP(&zabsCreateStepHoldFlags.jobid, "jobid", "j", "jobid for which the hold is installed")
	},
}

func doZabsCreateStep(ctx context.Context, sc *cli.Subcommand, args []string) error {
	if len(args) > 0 {
		return errors.New("subcommand takes no arguments")
	}

	f := &zabsCreateStepHoldFlags

	fs, _, _, err := zfs.DecomposeVersionString(f.target)
	if err != nil {
		return errors.Wrapf(err, "%q invalid target", f.target)
	}

	if f.jobid.FlagValue() == nil {
		return errors.Errorf("jobid must be set")
	}

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
