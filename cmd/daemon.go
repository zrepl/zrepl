package cmd

import (
	"github.com/spf13/cobra"
	"fmt"
	"sync"
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
	JobDo(log Logger) (err error)
}

func doDaemon(cmd *cobra.Command, args []string) {

	var wg sync.WaitGroup

	log.Printf("starting jobs from config")

	for _, job := range conf.Jobs {
		log.Printf("starting job %s", job.JobName())
		logger := jobLogger{log, job.JobName()}
		wg.Add(1)
		go func(j Job) {
			defer wg.Done()
			err := job.JobDo(logger)
			if err != nil {
				logger.Printf("returned error: %+v", err)
			}
		}(job)
	}

	log.Printf("waiting for jobs from config to finish")
	wg.Wait()

}
