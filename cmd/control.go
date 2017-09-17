package cmd

import (
	"context"
	"fmt"
	"github.com/spf13/cobra"
	"io"
	golog "log"
	"net"
	"net/http"
	"net/url"
	"os"
)

var controlCmd = &cobra.Command{
	Use:   "control",
	Short: "control zrepl daemon",
}

var pprofCmd = &cobra.Command{
	Use:   "pprof cpu OUTFILE",
	Short: "pprof CPU of daemon to OUTFILE (- for stdout)",
	Run:   doControlPProf,
}
var pprofCmdArgs struct {
	seconds int64
}

func init() {
	RootCmd.AddCommand(controlCmd)
	controlCmd.AddCommand(pprofCmd)
	pprofCmd.Flags().Int64Var(&pprofCmdArgs.seconds, "seconds", 30, "seconds to profile")
}

func doControlPProf(cmd *cobra.Command, args []string) {

	log := golog.New(os.Stderr, "", 0)

	die := func() {
		log.Printf("exiting after error")
		os.Exit(1)
	}

	ctx := context.WithValue(context.Background(), contextKeyLog, log)
	conf, err := ParseConfig(ctx, rootArgs.configFile)
	if err != nil {
		log.Printf("error parsing config: %s", err)
		die()
	}

	if cmd.Flags().Arg(0) != "cpu" {
		log.Printf("only CPU profiles are supported")
		log.Printf("%s", cmd.UsageString())
		die()
	}

	outfn := cmd.Flags().Arg(1)
	if outfn == "" {
		log.Printf("must specify output filename")
		log.Printf("%s", cmd.UsageString())
		die()
	}
	var out io.Writer
	if outfn == "-" {
		out = os.Stdout
	} else {
		out, err = os.Create(outfn)
		if err != nil {
			log.Printf("error creating output file: %s", err)
			die()
		}
	}

	log.Printf("connecting to daemon")
	httpc := http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", conf.Global.Control.Sockpath)
			},
		},
	}

	log.Printf("profiling...")
	v := url.Values{}
	v.Set("seconds", fmt.Sprintf("%d", pprofCmdArgs.seconds))
	v.Encode()
	resp, err := httpc.Get("http://unix" + ControlJobEndpointProfile + "?" + v.Encode())
	if err != nil {
		log.Printf("error: %s", err)
		die()
	}

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		log.Printf("error writing profile: %s", err)
		die()
	}

	log.Printf("finished")

}
