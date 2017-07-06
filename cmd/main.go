// zrepl replicates ZFS filesystems & volumes between pools
//
// Code Organization
//
// The cmd package uses github.com/spf13/cobra for its CLI.
//
// It combines the other packages in the zrepl project to implement zrepl functionality.
//
// Each subcommand's code is in the corresponding *.go file.
// All other *.go files contain code shared by the subcommands.
package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"github.com/zrepl/zrepl/jobrun"
	"golang.org/x/sys/unix"
	"io"
	golog "log"
	"net/http"
	_ "net/http/pprof"
	"os"
)

type Logger interface {
	Printf(format string, v ...interface{})
}

// global state / facilities
var (
	conf     Config
	runner   *jobrun.JobRunner
	logFlags int = golog.LUTC | golog.Ldate | golog.Ltime
	logOut   io.Writer
	log      Logger
)

var RootCmd = &cobra.Command{
	Use:   "zrepl",
	Short: "ZFS dataset replication",
	Long: `Replicate ZFS filesystems & volumes between pools:

  - push & pull mode
  - automatic snapshot creation & pruning
  - local / over the network
  - ACLs instead of blank SSH access`,
}

var rootArgs struct {
	configFile string
	stderrFile string
	httpPprof  string
}

func init() {
	cobra.OnInitialize(initConfig)
	RootCmd.PersistentFlags().StringVar(&rootArgs.configFile, "config", "", "config file path")
	RootCmd.PersistentFlags().StringVar(&rootArgs.stderrFile, "stderrFile", "-", "redirect stderr to given path")
	RootCmd.PersistentFlags().StringVar(&rootArgs.httpPprof, "debug.pprof.http", "", "run pprof http server on given port")
}

func initConfig() {

	// Logging & stderr redirection
	if rootArgs.stderrFile != "-" {
		file, err := os.OpenFile(rootArgs.stderrFile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			return
		}

		if err = unix.Dup2(int(file.Fd()), int(os.Stderr.Fd())); err != nil {
			file.WriteString(fmt.Sprintf("error redirecting stderr file %s: %s\n", rootArgs.stderrFile, err))
			return
		}
		logOut = file
	} else {
		logOut = os.Stderr
	}

	log = golog.New(logOut, "", logFlags)

	// CPU profiling
	if rootArgs.httpPprof != "" {
		go func() {
			http.ListenAndServe(rootArgs.httpPprof, nil)
		}()
	}

	// Config
	if rootArgs.configFile == "" {
		log.Printf("config file not set")
		os.Exit(1)
	}
	var err error
	if conf, err = ParseConfig(rootArgs.configFile); err != nil {
		log.Printf("error parsing config: %s", err)
		os.Exit(1)
	}

	jobrunLogger := golog.New(os.Stderr, "jobrun ", logFlags)
	runner = jobrun.NewJobRunner(jobrunLogger)
	return

}
