package cmd

import (
	"context"
	"fmt"
	"github.com/spf13/cobra"
	"github.com/zrepl/zrepl/logger"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// daemonCmd represents the daemon command
var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "start daemon",
	Run:   doDaemon,
}

func init() {
	RootCmd.AddCommand(daemonCmd)
}

type Job interface {
	JobName() string
	JobStart(ctxt context.Context)
}

func doDaemon(cmd *cobra.Command, args []string) {

	conf, err := ParseConfig(rootArgs.configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error parsing config: %s", err)
		os.Exit(1)
	}

	log := logger.NewLogger(conf.Global.logging.Outlets, 1*time.Second)

	log.Info(NewZreplVersionInformation().String())
	log.Debug("starting daemon")
	ctx := context.WithValue(context.Background(), contextKeyLog, log)
	ctx = context.WithValue(ctx, contextKeyLog, log)

	d := NewDaemon(conf)
	d.Loop(ctx)

}

type contextKey string

const (
	contextKeyLog contextKey = contextKey("log")
)

type Daemon struct {
	conf *Config
}

func NewDaemon(initialConf *Config) *Daemon {
	return &Daemon{initialConf}
}

func (d *Daemon) Loop(ctx context.Context) {

	log := ctx.Value(contextKeyLog).(Logger)

	ctx, cancel := context.WithCancel(ctx)

	sigChan := make(chan os.Signal, 1)
	finishs := make(chan Job)

	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	log.Info("starting jobs from config")
	i := 0
	for _, job := range d.conf.Jobs {
		logger := log.WithField(logJobField, job.JobName())
		logger.Info("starting")
		i++
		jobCtx := context.WithValue(ctx, contextKeyLog, logger)
		go func(j Job) {
			j.JobStart(jobCtx)
			finishs <- j
		}(job)
	}

	finishCount := 0
outer:
	for {
		select {
		case <-finishs:
			finishCount++
			if finishCount == len(d.conf.Jobs) {
				log.Info("all jobs finished")
				break outer
			}

		case sig := <-sigChan:
			log.WithField("signal", sig).Info("received signal")
			log.Info("cancelling all jobs")
			cancel()
		}
	}

	signal.Stop(sigChan)
	cancel() // make go vet happy

	log.Info("exiting")

}
