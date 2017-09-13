package cmd

import (
	"context"
	"fmt"
	"github.com/spf13/cobra"
	"os"
	"os/signal"
	"syscall"
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

type jobLogger struct {
	MainLog Logger
	JobName string
}

func (l jobLogger) Printf(format string, v ...interface{}) {
	l.MainLog.Printf(fmt.Sprintf("[%s]: %s", l.JobName, format), v...)
}

type Job interface {
	JobName() string
	JobStart(ctxt context.Context)
}

func doDaemon(cmd *cobra.Command, args []string) {
	d := Daemon{}
	d.Loop()
}

type contextKey string

const (
	contextKeyLog contextKey = contextKey("log")
)

type Daemon struct {
	log Logger
}

func (d *Daemon) Loop() {

	finishs := make(chan Job)
	cancels := make([]context.CancelFunc, len(conf.Jobs))

	log.Printf("starting jobs from config")
	i := 0
	for _, job := range conf.Jobs {
		log.Printf("starting job %s", job.JobName())

		logger := jobLogger{log, job.JobName()}
		ctx := context.Background()
		ctx, cancels[i] = context.WithCancel(ctx)
		i++
		ctx = context.WithValue(ctx, contextKeyLog, logger)

		go func(j Job) {
			job.JobStart(ctx)
			finishs <- j
		}(job)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	finishCount := 0
outer:
	for {
		select {
		case j := <-finishs:
			log.Printf("job finished: %s", j.JobName())
			finishCount++
			if finishCount == len(conf.Jobs) {
				log.Printf("all jobs finished")
				break outer
			}

		case sig := <-sigChan:
			log.Printf("received signal: %s", sig)
			log.Printf("cancelling all jobs")
			for _, c := range cancels {
				log.Printf("cancelling job")
				c()
			}
		}
	}

	signal.Stop(sigChan)

	log.Printf("exiting")

}
