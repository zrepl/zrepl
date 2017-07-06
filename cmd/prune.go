package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"github.com/zrepl/zrepl/util"
	"github.com/zrepl/zrepl/zfs"
	"os"
	"sort"
	"time"
)

var pruneArgs struct {
	job    string
	dryRun bool
}

var PruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "perform pruning",
	Run:   cmdPrune,
}

func init() {
	PruneCmd.Flags().StringVar(&pruneArgs.job, "job", "", "job to run")
	PruneCmd.Flags().BoolVarP(&pruneArgs.dryRun, "dryrun", "n", false, "dry run")
	RootCmd.AddCommand(PruneCmd)
}

func cmdPrune(cmd *cobra.Command, args []string) {

	jobFailed := false

	log.Printf("retending...")

	for _, prune := range conf.Prunes {

		if pruneArgs.job == "" || pruneArgs.job == prune.Name {
			log.Printf("Beginning prune job:\n%s", prune)
			ctx := PruneContext{prune, time.Now(), pruneArgs.dryRun}
			err := doPrune(ctx, log)
			if err != nil {
				jobFailed = true
				log.Printf("Prune job failed with error: %s", err)
			}
			log.Printf("\n")

		}

	}

	if jobFailed {
		log.Printf("At least one job failed with an error. Check log for details.")
		os.Exit(1)
	}

}

type PruneContext struct {
	Prune  Prune
	Now    time.Time
	DryRun bool
}

type retentionGridAdaptor struct {
	zfs.FilesystemVersion
}

func (a retentionGridAdaptor) Date() time.Time {
	return a.Creation
}

func (a retentionGridAdaptor) LessThan(b util.RetentionGridEntry) bool {
	return a.CreateTXG < b.(retentionGridAdaptor).CreateTXG
}

func doPrune(ctx PruneContext, log Logger) error {

	if ctx.DryRun {
		log.Printf("doing dry run")
	}

	prune := ctx.Prune

	// ZFSListSnapsFiltered --> todo can extend fsfilter or need new? Have already something per fs
	// Dedicated snapshot object? Adaptor object to FilesystemVersion?

	filesystems, err := zfs.ZFSListMapping(prune.DatasetFilter)
	if err != nil {
		return fmt.Errorf("error applying filesystem filter: %s", err)
	}

	for _, fs := range filesystems {

		fsversions, err := zfs.ZFSListFilesystemVersions(fs, prune.SnapshotFilter)
		if err != nil {
			return fmt.Errorf("error listing filesytem versions of %s: %s", fs, err)
		}

		if len(fsversions) == 0 {
			continue
		}

		adaptors := make([]util.RetentionGridEntry, len(fsversions))
		for fsv := range fsversions {
			adaptors[fsv] = retentionGridAdaptor{fsversions[fsv]}
		}

		sort.SliceStable(adaptors, func(i, j int) bool {
			return adaptors[i].LessThan(adaptors[j])
		})
		ctx.Now = adaptors[len(adaptors)-1].Date()

		describe := func(a retentionGridAdaptor) string {
			timeSince := a.Creation.Sub(ctx.Now)
			const day time.Duration = 24 * time.Hour
			days := timeSince / day
			remainder := timeSince % day
			return fmt.Sprintf("%s@%dd%s from now", a.ToAbsPath(fs), days, remainder)
		}

		keep, remove := prune.RetentionPolicy.FitEntries(ctx.Now, adaptors)
		for _, a := range remove {
			r := a.(retentionGridAdaptor)
			log.Printf("remove %s", describe(r))
			// do echo what we'll do and exec zfs destroy if not dry run
			// special handling for EBUSY (zfs hold)
			// error handling for clones? just echo to cli, skip over, and exit with non-zero status code (we're idempotent)
			if !ctx.DryRun {
				err := zfs.ZFSDestroy(r.ToAbsPath(fs))
				if err != nil {
					// handle
					log.Printf("error: %s", err)
				}
			}
		}
		for _, a := range keep {
			r := a.(retentionGridAdaptor)
			log.Printf("would keep %s", describe(r))
		}
	}

	return nil

}
