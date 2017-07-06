package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"github.com/zrepl/zrepl/jobrun"
	"github.com/zrepl/zrepl/zfs"
	"sync"
	"time"
)

var autosnapArgs struct {
	job string
}

var AutosnapCmd = &cobra.Command{
	Use:   "autosnap",
	Short: "perform automatic snapshotting",
	Run:   cmdAutosnap,
}

func init() {
	AutosnapCmd.Flags().StringVar(&autosnapArgs.job, "job", "", "job to run")
	RootCmd.AddCommand(AutosnapCmd)
}

func cmdAutosnap(cmd *cobra.Command, args []string) {

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runner.Start()
	}()

	log.Printf("autosnap...")

	for i := range conf.Autosnaps {

		snap := conf.Autosnaps[i]

		if autosnapArgs.job == "" || autosnapArgs.job == snap.Name {

			job := jobrun.Job{
				Name:           fmt.Sprintf("autosnap.%s", snap.Name),
				RepeatStrategy: snap.Interval,
				RunFunc: func(log jobrun.Logger) error {
					log.Printf("doing autosnap: %v", snap)
					ctx := AutosnapContext{snap}
					return doAutosnap(ctx, log)
				},
			}
			runner.AddJob(job)

		}
	}

	wg.Wait()

}

type AutosnapContext struct {
	Autosnap Autosnap
}

func doAutosnap(ctx AutosnapContext, log Logger) (err error) {

	snap := ctx.Autosnap

	filesystems, err := zfs.ZFSListMapping(snap.DatasetFilter)
	if err != nil {
		return fmt.Errorf("cannot filter datasets: %s", err)
	}

	suffix := time.Now().In(time.UTC).Format("20060102_150405_000")
	snapname := fmt.Sprintf("%s%s", snap.Prefix, suffix)

	hadError := false

	for _, fs := range filesystems { // optimization: use recursive snapshots / channel programs here
		log.Printf("snapshotting filesystem %s@%s", fs, snapname)
		err := zfs.ZFSSnapshot(fs, snapname, false)
		if err != nil {
			log.Printf("error snapshotting %s: %s", fs, err)
			hadError = true
		}
	}

	if hadError {
		err = fmt.Errorf("errors occurred during autosnap, check logs for details")
	}

	return

}
