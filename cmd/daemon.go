package cmd

import (
	"github.com/spf13/cobra"
	"github.com/zrepl/zrepl/jobrun"
)

var daemonArgs struct {
	noPrune    bool
	noAutosnap bool
	noPull     bool
}

// daemonCmd represents the daemon command
var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "run zrepl as a daemon",
	Run:   doDaemon,
}

func init() {
	RootCmd.AddCommand(daemonCmd)
	daemonCmd.Flags().BoolVar(&daemonArgs.noPrune, "noprune", false, "don't run prune jobs")
	daemonCmd.Flags().BoolVar(&daemonArgs.noAutosnap, "noautosnap", false, "don't run autosnap jobs")
	daemonCmd.Flags().BoolVar(&daemonArgs.noPull, "nopull", false, "don't run pull jobs")
}

func doDaemon(cmd *cobra.Command, args []string) {

	r := jobrun.NewJobRunner(log)
	rc := make(chan jobrun.JobEvent)
	r.SetNotificationChannel(rc)
	go r.Run()

	if !daemonArgs.noAutosnap {
		for name := range conf.Autosnaps {
			as := conf.Autosnaps[name]
			r.AddJob(as)
		}
	}

	if !daemonArgs.noPrune {
		for name := range conf.Prunes {
			p := conf.Prunes[name]
			r.AddJob(p)
		}
	}

	if !daemonArgs.noPull {
		for name := range conf.Pulls {
			p := conf.Pulls[name]
			r.AddJob(p)
		}
	}

	for {
		event := <-rc
		//		log.Printf("received event: %T", event)
		switch e := (event).(type) {
		case jobrun.JobFinishedEvent:
			//log.Printf("[%s] job run finished after %s", e.Job.JobName(), e.Result.RunTime())
			if e.Result.Error != nil {
				log.Printf("[%s] exited with error: %s", e.Result.Error)
			}
		case jobrun.JobScheduledEvent:
			//log.Printf("[%s] scheduled to run at %s", e.Job.JobName(), e.DueAt)
		case jobrun.JobrunIdleEvent:
			//log.Printf("sleeping until %v", e.SleepUntil)
		case jobrun.JobrunFinishedEvent:
			//log.Printf("no more jobs")
			break
		}
	}

}
