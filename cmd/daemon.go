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
	JobType() JobType
	JobStart(ctxt context.Context)
}

type JobType string

const (
	JobTypePull       JobType = "pull"
	JobTypeSource     JobType = "source"
	JobTypeLocal      JobType = "local"
	JobTypePrometheus JobType = "prometheus"
	JobTypeControl    JobType = "control"
)

func ParseUserJobType(s string) (JobType, error) {
	switch s {
	case "pull":
		return JobTypePull, nil
	case "source":
		return JobTypeSource, nil
	case "local":
		return JobTypeLocal, nil
	case "prometheus":
		return JobTypePrometheus, nil
	}
	return "", fmt.Errorf("unknown job type '%s'", s)
}

func (j JobType) String() string {
	return string(j)
}

func doDaemon(cmd *cobra.Command, args []string) {

	conf, err := ParseConfig(rootArgs.configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error parsing config: %s\n", err)
		os.Exit(1)
	}

	log := logger.NewLogger(conf.Global.logging.Outlets, 1*time.Second)

	log.Info(NewZreplVersionInformation().String())
	log.Debug("starting daemon")
	ctx := WithLogger(context.Background(), log)

	d := NewDaemon(conf)
	d.Loop(ctx)

}

type contextKey string

const (
	contextKeyLog    contextKey = contextKey("log")
	contextKeyDaemon contextKey = contextKey("daemon")
)

func getLogger(ctx context.Context) Logger {
	return ctx.Value(contextKeyLog).(Logger)
}

func WithLogger(ctx context.Context, l Logger) context.Context {
	return context.WithValue(ctx, contextKeyLog, l)
}

type Daemon struct {
	conf      *Config
	startedAt time.Time
}

func NewDaemon(initialConf *Config) *Daemon {
	return &Daemon{conf: initialConf}
}

func (d *Daemon) Loop(ctx context.Context) {

	d.startedAt = time.Now()

	log := getLogger(ctx)

	ctx, cancel := context.WithCancel(ctx)
	ctx = context.WithValue(ctx, contextKeyDaemon, d)

	sigChan := make(chan os.Signal, 1)
	finishs := make(chan Job)

	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	log.Info("starting jobs from config")
	i := 0
	for _, job := range d.conf.Jobs {
		logger := log.WithField(logJobField, job.JobName())
		logger.Info("starting")
		i++
		jobCtx := WithLogger(ctx, logger)
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
