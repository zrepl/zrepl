package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/zrepl/zrepl/logger"
	"io"
	golog "log"
	"net"
	"net/http"
	"os"
	"github.com/zrepl/zrepl/version"
	"github.com/zrepl/zrepl/cmd/daemon"
)

var controlCmd = &cobra.Command{
	Use:   "control",
	Short: "control zrepl daemon",
}

var pprofCmd = &cobra.Command{
	Use:   "pprof off | [on TCP_LISTEN_ADDRESS]",
	Short: "start a http server exposing go-tool-compatible profiling endpoints at TCP_LISTEN_ADDRESS",
	Run:   doControlPProf,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().NArg() < 1 {
			goto enargs
		}
		switch cmd.Flags().Arg(0) {
		case "on":
			pprofCmdArgs.msg.Run = true
			if cmd.Flags().NArg() != 2 {
				return errors.New("must specify TCP_LISTEN_ADDRESS as second positional argument")
			}
			pprofCmdArgs.msg.HttpListenAddress = cmd.Flags().Arg(1)
		case "off":
			if cmd.Flags().NArg() != 1 {
				goto enargs
			}
			pprofCmdArgs.msg.Run = false
		}
		return nil
	enargs:
		return errors.New("invalid number of positional arguments")

	},
}
var pprofCmdArgs struct {
	msg daemon.PprofServerControlMsg
}

var controlVersionCmd = &cobra.Command{
	Use:   "version",
	Short: "print version of running zrepl daemon",
	Run:   doControLVersionCmd,
}

var controlStatusCmdArgs struct {
	format      string
	level       logger.Level
	onlyShowJob string
}

func init() {
	RootCmd.AddCommand(controlCmd)
	controlCmd.AddCommand(pprofCmd)
	controlCmd.AddCommand(controlVersionCmd)
	controlStatusCmdArgs.level = logger.Warn
}

func controlHttpClient() (client http.Client, err error) {

	conf, err := ParseConfig(rootArgs.configFile)
	if err != nil {
		return http.Client{}, err
	}

	return http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", conf.Global.Control.Sockpath)
			},
		},
	}, nil
}

func doControlPProf(cmd *cobra.Command, args []string) {

	log := golog.New(os.Stderr, "", 0)

	die := func() {
		log.Printf("exiting after error")
		os.Exit(1)
	}

	log.Printf("connecting to zrepl daemon")
	httpc, err := controlHttpClient()
	if err != nil {
		log.Printf("error parsing config: %s", err)
		die()
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(&pprofCmdArgs.msg); err != nil {
		log.Printf("error marshaling request: %s", err)
		die()
	}
	_, err = httpc.Post("http://unix"+daemon.ControlJobEndpointPProf, "application/json", &buf)
	if err != nil {
		log.Printf("error: %s", err)
		die()
	}

	log.Printf("finished")
}

func doControLVersionCmd(cmd *cobra.Command, args []string) {

	log := golog.New(os.Stderr, "", 0)

	die := func() {
		log.Printf("exiting after error")
		os.Exit(1)
	}

	httpc, err := controlHttpClient()
	if err != nil {
		log.Printf("could not connect to daemon: %s", err)
		die()
	}

	resp, err := httpc.Get("http://unix" + daemon.ControlJobEndpointVersion)
	if err != nil {
		log.Printf("error: %s", err)
		die()
	} else if resp.StatusCode != http.StatusOK {
		var msg bytes.Buffer
		io.CopyN(&msg, resp.Body, 4096)
		log.Printf("error: %s", msg.String())
		die()
	}

	var info version.ZreplVersionInformation
	err = json.NewDecoder(resp.Body).Decode(&info)
	if err != nil {
		log.Printf("error unmarshaling response: %s", err)
		die()
	}

	fmt.Println(info.String())

}
