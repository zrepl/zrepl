package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/zrepl/zrepl/logger"
	"io"
	golog "log"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
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
	msg PprofServerControlMsg
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

var controlStatusCmd = &cobra.Command{
	Use:   "status [JOB_NAME]",
	Short: "get current status",
	Run:   doControlStatusCmd,
}

func init() {
	RootCmd.AddCommand(controlCmd)
	controlCmd.AddCommand(pprofCmd)
	controlCmd.AddCommand(controlVersionCmd)
	controlCmd.AddCommand(controlStatusCmd)
	controlStatusCmd.Flags().StringVar(&controlStatusCmdArgs.format, "format", "human", "output format (human|raw)")
	controlStatusCmdArgs.level = logger.Warn
	controlStatusCmd.Flags().Var(&controlStatusCmdArgs.level, "level", "minimum log level to show")
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
	_, err = httpc.Post("http://unix"+ControlJobEndpointPProf, "application/json", &buf)
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

	resp, err := httpc.Get("http://unix" + ControlJobEndpointVersion)
	if err != nil {
		log.Printf("error: %s", err)
		die()
	} else if resp.StatusCode != http.StatusOK {
		var msg bytes.Buffer
		io.CopyN(&msg, resp.Body, 4096)
		log.Printf("error: %s", msg.String())
		die()
	}

	var info ZreplVersionInformation
	err = json.NewDecoder(resp.Body).Decode(&info)
	if err != nil {
		log.Printf("error unmarshaling response: %s", err)
		die()
	}

	fmt.Println(info.String())

}

func doControlStatusCmd(cmd *cobra.Command, args []string) {

	log := golog.New(os.Stderr, "", 0)

	die := func() {
		log.Print("exiting after error")
		os.Exit(1)
	}

	if len(args) == 1 {
		controlStatusCmdArgs.onlyShowJob = args[0]
	} else if len(args) > 1 {
		log.Print("can only specify one job as positional argument")
		cmd.Usage()
		die()
	}

	httpc, err := controlHttpClient()
	if err != nil {
		log.Printf("could not connect to daemon: %s", err)
		die()
	}

	resp, err := httpc.Get("http://unix" + ControlJobEndpointStatus)
	if err != nil {
		log.Printf("error: %s", err)
		die()
	} else if resp.StatusCode != http.StatusOK {
		var msg bytes.Buffer
		io.CopyN(&msg, resp.Body, 4096)
		log.Printf("error: %s", msg.String())
		die()
	}

	var status DaemonStatus
	err = json.NewDecoder(resp.Body).Decode(&status)
	if err != nil {
		log.Printf("error unmarshaling response: %s", err)
		die()
	}

	switch controlStatusCmdArgs.format {
	case "raw":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(status); err != nil {
			log.Panic(err)
		}
	case "human":

		formatter := HumanFormatter{}
		formatter.SetMetadataFlags(MetadataAll)
		formatter.SetIgnoreFields([]string{
			logJobField,
		})
		jobNames := make([]string, 0, len(status.Jobs))
		for name, _ := range status.Jobs {
			jobNames = append(jobNames, name)
		}
		sort.Slice(jobNames, func(i, j int) bool {
			return strings.Compare(jobNames[i], jobNames[j]) == -1
		})
		now := time.Now()
		for _, name := range jobNames {

			if controlStatusCmdArgs.onlyShowJob != "" && name != controlStatusCmdArgs.onlyShowJob {
				continue
			}

			job := status.Jobs[name]
			jobLogEntries := make([]logger.Entry, 0)
			informAboutError := false

			fmt.Printf("Job '%s':\n", name)
			for _, task := range job.Tasks {

				var header bytes.Buffer
				fmt.Fprintf(&header, "  Task '%s': ", task.Name)
				if !task.Idle {
					fmt.Fprint(&header, strings.Join(task.ActivityStack, "."))
				} else {
					fmt.Fprint(&header, "<idle>")
				}
				fmt.Fprint(&header, " ")
				const TASK_STALLED_HOLDOFF_DURATION = 10 * time.Second
				sinceLastUpdate := now.Sub(task.LastUpdate)
				if !task.Idle || task.ProgressRx != 0 || task.ProgressTx != 0 {
					fmt.Fprintf(&header, "(%s / %s , Rx/Tx",
						humanize.Bytes(uint64(task.ProgressRx)),
						humanize.Bytes(uint64(task.ProgressTx)))
					if task.Idle {
						fmt.Fprint(&header, ", values from last run")
					}
					fmt.Fprint(&header, ")")
				}
				fmt.Fprint(&header, "\n")
				if !task.Idle && !task.LastUpdate.IsZero() && sinceLastUpdate >= TASK_STALLED_HOLDOFF_DURATION {
					informAboutError = true
					fmt.Fprintf(&header, "    WARNING: last update %s ago at %s)",
						sinceLastUpdate.String(),
						task.LastUpdate.Format(HumanFormatterDateFormat))
					fmt.Fprint(&header, "\n")
				}
				io.Copy(os.Stdout, &header)

				jobLogEntries = append(jobLogEntries, task.LogEntries...)
				informAboutError = informAboutError || task.MaxLogLevel >= logger.Warn
			}

			sort.Slice(jobLogEntries, func(i, j int) bool {
				return jobLogEntries[i].Time.Before(jobLogEntries[j].Time)
			})
			if informAboutError {
				fmt.Println("    WARNING: Some tasks encountered problems since the last time they left idle state:")
				fmt.Println("             check the logs below or your log file for more information.")
				fmt.Println("             Use the --level flag if you need debug information.")
				fmt.Println()
			}
			for _, e := range jobLogEntries {
				if e.Level < controlStatusCmdArgs.level {
					continue
				}
				formatted, err := formatter.Format(&e)
				if err != nil {
					panic(err)
				}
				fmt.Printf("      %s\n", string(formatted))
			}
			fmt.Println()

		}
	default:
		log.Printf("invalid output format '%s'", controlStatusCmdArgs.format)
		die()
	}

}
