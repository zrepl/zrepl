package client

import (
	"context"
	"fmt"

	"github.com/fatih/color"
	"github.com/kr/pretty"
	"github.com/pkg/errors"
	"github.com/spf13/pflag"

	"github.com/zrepl/zrepl/daemon/job"
	"github.com/zrepl/zrepl/endpoint"
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

var migrations = []*cli.Subcommand{
	&cli.Subcommand{
		Use: "0.0.X:0.1:placeholder",
		Run: doMigratePlaceholder0_1,
		SetupFlags: func(f *pflag.FlagSet) {
			f.BoolVar(&migratePlaceholder0_1Args.dryRun, "dry-run", false, "dry run")
		},
	},
	&cli.Subcommand{
		Use: "replication-cursor:v1-v2",
		Run: doMigrateReplicationCursor,
		SetupFlags: func(f *pflag.FlagSet) {
			f.BoolVar(&migrateReplicationCursorArgs.dryRun, "dry-run", false, "dry run")
		},
	},
}

var migratePlaceholder0_1Args struct {
	dryRun bool
}

func doMigratePlaceholder0_1(ctx context.Context, sc *cli.Subcommand, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("migration does not take arguments, got %v", args)
	}

	cfg := sc.Config()

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
			r, err := zfs.ZFSMigrateHashBasedPlaceholderToCurrent(ctx, fs, migratePlaceholder0_1Args.dryRun)
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

var migrateReplicationCursorArgs struct {
	dryRun bool
}

var bold = color.New(color.Bold)
var succ = color.New(color.FgGreen)
var fail = color.New(color.FgRed)

var migrateReplicationCursorSkipSentinel = fmt.Errorf("skipping this filesystem")

func doMigrateReplicationCursor(ctx context.Context, sc *cli.Subcommand, args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("migration does not take arguments, got %v", args)
	}

	cfg := sc.Config()
	jobs, err := job.JobsFromConfig(cfg, config.ParseFlagsNone)
	if err != nil {
		fmt.Printf("cannot parse config:\n%s\n\n", err)
		fmt.Printf("NOTE: this migration was released together with a change in job name requirements.\n")
		return fmt.Errorf("exiting migration after error")
	}

	v1cursorJobs := make([]job.Job, 0, len(cfg.Jobs))
	for i, j := range cfg.Jobs {
		if jobs[i].Name() != j.Name() {
			panic("implementation error")
		}
		switch j.Ret.(type) {
		case *config.PushJob:
			v1cursorJobs = append(v1cursorJobs, jobs[i])
		case *config.SourceJob:
			v1cursorJobs = append(v1cursorJobs, jobs[i])
		default:
			fmt.Printf("ignoring job %q (%d/%d, type %T), not supposed to create v1 replication cursors\n", j.Name(), i, len(cfg.Jobs), j.Ret)
			continue
		}
	}

	// scan all filesystems for v1 replication cursors

	fss, err := zfs.ZFSListMapping(ctx, zfs.NoFilter())
	if err != nil {
		return errors.Wrap(err, "list filesystems")
	}

	var hadError bool
	for _, fs := range fss {

		bold.Printf("INSPECT FILESYSTEM %q\n", fs.ToString())

		err := doMigrateReplicationCursorFS(ctx, v1cursorJobs, fs)
		if err == migrateReplicationCursorSkipSentinel {
			bold.Printf("FILESYSTEM SKIPPED\n")
		} else if err != nil {
			hadError = true
			fail.Printf("MIGRATION FAILED: %s\n", err)
		} else {
			succ.Printf("FILESYSTEM %q COMPLETE\n", fs.ToString())
		}
	}

	if hadError {
		fail.Printf("\n\none or more filesystems could not be migrated, please inspect output and or re-run migration")
		return errors.Errorf("")
	}
	return nil
}

func doMigrateReplicationCursorFS(ctx context.Context, v1CursorJobs []job.Job, fs *zfs.DatasetPath) error {

	var owningJob job.Job = nil
	for _, job := range v1CursorJobs {
		conf := job.SenderConfig()
		if conf == nil {
			continue
		}
		pass, err := conf.FSF.Filter(fs)
		if err != nil {
			return errors.Wrapf(err, "filesystem filter error in job %q for fs %q", job.Name(), fs.ToString())
		}
		if !pass {
			continue
		}
		if owningJob != nil {
			return errors.Errorf("jobs %q and %q both match %q\ncannot attribute replication cursor to either one", owningJob.Name(), job.Name(), fs)
		}
		owningJob = job
	}
	if owningJob == nil {
		fmt.Printf("no job's Filesystems filter matches\n")
		return migrateReplicationCursorSkipSentinel
	}
	fmt.Printf("identified owning job %q\n", owningJob.Name())

	bookmarks, err := zfs.ZFSListFilesystemVersions(ctx, fs, zfs.ListFilesystemVersionsOptions{
		Types: zfs.Bookmarks,
	})
	if err != nil {
		return errors.Wrapf(err, "list filesystem versions of %q", fs.ToString())
	}

	var oldCursor *zfs.FilesystemVersion
	for i, fsv := range bookmarks {
		_, _, err := endpoint.ParseReplicationCursorBookmarkName(fsv.ToAbsPath(fs))
		if err != endpoint.ErrV1ReplicationCursor {
			continue
		}

		if oldCursor != nil {
			fmt.Printf("unexpected v1 replication cursor candidate: %q", fsv.ToAbsPath(fs))
			return errors.Wrap(err, "multiple filesystem versions identified as v1 replication cursors")
		}

		oldCursor = &bookmarks[i]

	}

	if oldCursor == nil {
		bold.Printf("no v1 replication cursor found for filesystem %q\n", fs.ToString())
		return migrateReplicationCursorSkipSentinel
	}

	fmt.Printf("found v1 replication cursor:\n%s\n", pretty.Sprint(oldCursor))

	mostRecentNew, err := endpoint.GetMostRecentReplicationCursorOfJob(ctx, fs.ToString(), owningJob.SenderConfig().JobID)
	if err != nil {
		return errors.Wrapf(err, "get most recent v2 replication cursor")
	}

	if mostRecentNew == nil {
		return errors.Errorf("no v2 replication cursor found for job %q on filesystem %q", owningJob.SenderConfig().JobID, fs.ToString())
	}

	fmt.Printf("most recent v2 replication cursor:\n%#v", oldCursor)

	if !(mostRecentNew.CreateTXG >= oldCursor.CreateTXG) {
		return errors.Errorf("v1 replication cursor createtxg is higher than v2 cursor's, skipping this filesystem")
	}

	fmt.Printf("determined that v2 cursor is bookmark of same or newer version than v1 cursor\n")
	fmt.Printf("destroying v1 cursor %q\n", oldCursor.ToAbsPath(fs))

	if migrateReplicationCursorArgs.dryRun {
		succ.Printf("DRY RUN\n")
	} else {
		if err := zfs.ZFSDestroyFilesystemVersion(ctx, fs, oldCursor); err != nil {
			return err
		}
	}

	return nil
}
