package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
	"github.com/zrepl/zrepl/logger"
	"io"
	golog "log"
	"net"
	"net/http"
	"net/url"
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
	Use:   "pprof cpu OUTFILE",
	Short: "pprof CPU of daemon to OUTFILE (- for stdout)",
	Run:   doControlPProf,
}
var pprofCmdArgs struct {
	seconds int64
}

var controlVersionCmd = &cobra.Command{
	Use:   "version",
	Short: "print version of running zrepl daemon",
	Run:   doControLVersionCmd,
}

var controlStatusCmdArgs struct {
	format         string
	alwaysShowLogs bool
	onlyShowJob    string
}

var controlStatusCmd = &cobra.Command{
	Use:   "status [JOB_NAME]",
	Short: "get current status",
	Run:   doControlStatusCmd,
}

func init() {
	RootCmd.AddCommand(controlCmd)
	controlCmd.AddCommand(pprofCmd)
	pprofCmd.Flags().Int64Var(&pprofCmdArgs.seconds, "seconds", 30, "seconds to profile")
	controlCmd.AddCommand(controlVersionCmd)
	controlCmd.AddCommand(controlStatusCmd)
	controlStatusCmd.Flags().StringVar(&controlStatusCmdArgs.format, "format", "human", "output format (human|raw)")
	controlStatusCmd.Flags().BoolVar(&controlStatusCmdArgs.alwaysShowLogs, "always-show-logs", false, "always show logs, even if no error occured")
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
	var err error
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
	httpc, err := controlHttpClient()
	if err != nil {
		log.Printf("error parsing config: %s", err)
		die()
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

			showLog := informAboutError || controlStatusCmdArgs.alwaysShowLogs

			if showLog {
				sort.Slice(jobLogEntries, func(i, j int) bool {
					return jobLogEntries[i].Time.Before(jobLogEntries[j].Time)
				})
				if informAboutError {
					fmt.Println("    WARNING: Some tasks encountered problems since the last time they left idle state:")
					fmt.Println("             check the logs below or your log file for more information.")
				}
				for _, e := range jobLogEntries {
					formatted, err := formatter.Format(&e)
					if err != nil {
						panic(err)
					}
					fmt.Printf("      %s\n", string(formatted))
				}
				fmt.Println()
			}

		}
	default:
		log.Printf("invalid output format '%s'", controlStatusCmdArgs.format)
		die()
	}

}
