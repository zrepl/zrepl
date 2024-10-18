package client

import (
	"context"
	"io"
	"log"
	"os"

	"golang.org/x/net/websocket"

	"github.com/zrepl/zrepl/internal/cli"
)

var pprofActivityTraceCmd = &cli.Subcommand{
	Use:   "activity-trace ZREPL_PPROF_HOST:ZREPL_PPROF_PORT",
	Short: "attach to zrepl daemon with activated pprof listener and dump an activity-trace to stdout",
	Run:   runPProfActivityTrace,
}

func runPProfActivityTrace(ctx context.Context, subcommand *cli.Subcommand, args []string) error {
	log := log.New(os.Stderr, "", 0)

	die := func() {
		log.Printf("exiting after error")
		os.Exit(1)
	}

	if len(args) != 1 {
		log.Printf("exactly one positional argument is required")
		die()
	}

	url := "ws://" + args[0] + "/debug/zrepl/activity-trace" // FIXME dont' repeat that

	log.Printf("attaching to activity trace stream %s", url)
	ws, err := websocket.Dial(url, "", url)
	if err != nil {
		log.Printf("error: %s", err)
		die()
	}

	_, err = io.Copy(os.Stdout, ws)
	return err
}
