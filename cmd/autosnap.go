package cmd

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/zrepl/zrepl/jobrun"
	"github.com/zrepl/zrepl/zfs"
)

var AutosnapCmd = &cobra.Command{
	Use:   "autosnap",
	Short: "perform automatic snapshotting",
	Run:   cmdAutosnap,
}

func init() {
	RootCmd.AddCommand(AutosnapCmd)
}

func cmdAutosnap(cmd *cobra.Command, args []string) {

	var wg sync.WaitGroup

	r := jobrun.NewJobRunner(log)
	wg.Add(1)
	go func() {
		r.Run()
		wg.Done()
	}()

	if len(args) < 1 {
		log.Printf("must specify exactly one job as positional argument")
		os.Exit(1)
	}

	snap, ok := conf.Autosnaps[args[0]]
	if !ok {
		log.Printf("could not find autosnap job: %s", args[0])
		os.Exit(1)
	}

	r.AddJob(snap)

	wg.Wait()

}

type AutosnapContext struct {
	Autosnap *Autosnap
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
		log.Printf("snapshotting filesystem %s@%s", fs.ToString(), snapname)
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
