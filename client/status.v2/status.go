package status

import (
	"context"

	"github.com/pkg/errors"
	"github.com/spf13/pflag"

	"github.com/zrepl/zrepl/cli"
	"github.com/zrepl/zrepl/client/status.v2/client"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon"
	"github.com/zrepl/zrepl/util/choices"
)

type Client interface {
	Status() (daemon.Status, error)
	StatusRaw() ([]byte, error)
	SignalWakeup(job string) error
	SignalSnapshot(job string) error
	SignalReset(job string) error
}

var statusv2Flags struct {
	Mode choices.Choices
	Job  string
}

type statusv2Mode int

const (
	StatusV2ModeInteractive statusv2Mode = 1 + iota
	StatusV2ModeDump
	StatusV2ModeRaw
)

var Subcommand = &cli.Subcommand{
	Use:   "status-v2",
	Short: "start status-v2 terminal UI",
	SetupFlags: func(f *pflag.FlagSet) {
		statusv2Flags.Mode.Init(
			"interactive", StatusV2ModeInteractive,
			"dump", StatusV2ModeDump,
			"raw", StatusV2ModeRaw,
		)
		statusv2Flags.Mode.SetTypeString("mode")
		statusv2Flags.Mode.SetDefaultValue(StatusV2ModeInteractive)
		f.Var(&statusv2Flags.Mode, "mode", statusv2Flags.Mode.Usage())
		f.StringVar(&statusv2Flags.Job, "job", "", "only dump specified job (only works in \"dump\" mode)")
	},
	Run: func(ctx context.Context, subcommand *cli.Subcommand, args []string) error {
		return runStatusV2Command(ctx, subcommand.Config(), args)
	},
}

func runStatusV2Command(ctx context.Context, config *config.Config, args []string) error {

	c, err := client.New("unix", config.Global.Control.SockPath)
	if err != nil {
		return errors.Wrapf(err, "connect to daemon socket at %q", config.Global.Control.SockPath)
	}

	switch statusv2Flags.Mode.Value().(statusv2Mode) {
	case StatusV2ModeInteractive:
		return interactive(c)
	case StatusV2ModeDump:
		return dump(c, statusv2Flags.Job)
	case StatusV2ModeRaw:
		return raw(c)
	default:
		panic("unreachable")
	}
}
