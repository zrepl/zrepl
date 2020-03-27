package status

import (
	"context"
	"os"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/pkg/errors"
	"github.com/spf13/pflag"

	"github.com/zrepl/zrepl/cli"
	"github.com/zrepl/zrepl/client/status/client"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon"
	"github.com/zrepl/zrepl/util/choices"
)

type Client interface {
	Status() (daemon.Status, error)
	StatusRaw() ([]byte, error)
	SignalReplication(job string) error
	SignalSnapshot(job string) error
	SignalReset(job string) error
}

type statusFlags struct {
	Mode  choices.Choices
	Job   string
	Delay time.Duration
}

var statusv2Flags statusFlags

type statusv2Mode int

const (
	StatusV2ModeInteractive statusv2Mode = 1 + iota
	StatusV2ModeDump
	StatusV2ModeRaw
	StatusV2ModeLegacy
)

var Subcommand = &cli.Subcommand{
	Use:   "status",
	Short: "retrieve & display daemon status information",
	SetupFlags: func(f *pflag.FlagSet) {
		statusv2Flags.Mode.Init(
			"interactive", StatusV2ModeInteractive,
			"dump", StatusV2ModeDump,
			"raw", StatusV2ModeRaw,
			"legacy", StatusV2ModeLegacy,
		)
		statusv2Flags.Mode.SetTypeString("mode")
		statusv2Flags.Mode.SetDefaultValue(StatusV2ModeInteractive)
		f.Var(&statusv2Flags.Mode, "mode", statusv2Flags.Mode.Usage())
		f.StringVar(&statusv2Flags.Job, "job", "", "only show specified job (works in \"dump\" and \"interactive\" mode)")
		f.DurationVarP(&statusv2Flags.Delay, "delay", "d", 1*time.Second, "use -d 3s for 3 seconds delay (minimum delay is 1s)")
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

	mode := statusv2Flags.Mode.Value().(statusv2Mode)

	if !isatty.IsTerminal(os.Stdout.Fd()) && mode != StatusV2ModeDump {
		usemode, err := statusv2Flags.Mode.InputForChoice(StatusV2ModeDump)
		if err != nil {
			panic(err)
		}
		return errors.Errorf("error: stdout is not a tty, please use --mode %s", usemode)
	}

	switch mode {
	case StatusV2ModeInteractive:
		return interactive(c, statusv2Flags)
	case StatusV2ModeDump:
		return dump(c, statusv2Flags.Job)
	case StatusV2ModeRaw:
		return raw(c)
	case StatusV2ModeLegacy:
		return legacy(c, statusv2Flags)
	default:
		panic("unreachable")
	}
}
