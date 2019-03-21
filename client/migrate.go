package client

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"github.com/spf13/pflag"
	"github.com/zrepl/zrepl/zfs"

	"github.com/zrepl/zrepl/cli"
	"github.com/zrepl/zrepl/config"
)

var (
	MigrateCmd = &cli.Subcommand{
		Use:   "migrate",
		Short: "perform migration of the on-disk / zfs properties",
		SetupSubcommands: func() []*cli.Subcommand {
			return migrations
		},
	}
)

type migration struct {
	name   string
	method func(config *config.Config, args []string) error
}

var migrations = []*cli.Subcommand{
	&cli.Subcommand{
		Use: "0.0.X:0.1:placeholder",
		Run: doMigratePlaceholder0_1,
		SetupFlags: func(f *pflag.FlagSet) {
			f.BoolVar(&migratePlaceholder0_1Args.dryRun, "dry-run", false, "dry run")
		},
	},
}

var migratePlaceholder0_1Args struct {
	dryRun bool
}

func doMigratePlaceholder0_1(sc *cli.Subcommand, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("migration does not take arguments, got %v", args)
	}

	cfg := sc.Config()

	ctx := context.Background()
	allFSS, err := zfs.ZFSListMapping(ctx, zfs.NoFilter())
	if err != nil {
		return errors.Wrap(err, "cannot list filesystems")
	}

	type workItem struct {
		jobName string
		rootFS  *zfs.DatasetPath
		fss     []*zfs.DatasetPath
	}
	var wis []workItem
	for i, j := range cfg.Jobs {
		var rfsS string
		switch job := j.Ret.(type) {
		case *config.SinkJob:
			rfsS = job.RootFS
		case *config.PullJob:
			rfsS = job.RootFS
		default:
			fmt.Printf("ignoring job %q (%d/%d, type %T)\n", j.Name(), i, len(cfg.Jobs), j.Ret)
			continue
		}
		rfs, err := zfs.NewDatasetPath(rfsS)
		if err != nil {
			return errors.Wrapf(err, "root fs for job %q is not a valid dataset path", j.Name())
		}
		var fss []*zfs.DatasetPath
		for _, fs := range allFSS {
			if fs.HasPrefix(rfs) {
				fss = append(fss, fs)
			}
		}
		wis = append(wis, workItem{j.Name(), rfs, fss})
	}

	for _, wi := range wis {
		fmt.Printf("job %q => migrate filesystems below root_fs %q\n", wi.jobName, wi.rootFS.ToString())
		if len(wi.fss) == 0 {
			fmt.Printf("\tno filesystems\n")
			continue
		}
		for _, fs := range wi.fss {
			fmt.Printf("\t%q ... ", fs.ToString())
			r, err := zfs.ZFSMigrateHashBasedPlaceholderToCurrent(fs, migratePlaceholder0_1Args.dryRun)
			if err != nil {
				fmt.Printf("error: %s\n", err)
			} else if !r.NeedsModification {
				fmt.Printf("unchanged (placeholder=%v)\n", r.OriginalState.IsPlaceholder)
			} else {
				fmt.Printf("migrate (placeholder=%v) (old value = %q)\n",
					r.OriginalState.IsPlaceholder, r.OriginalState.RawLocalPropertyValue)
			}
		}
	}

	return nil
}
