package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/zrepl/zrepl/util"
	"github.com/zrepl/zrepl/zfs"
	"time"
)

type Pruner struct {
	Now            time.Time
	DryRun         bool
	DatasetFilter  zfs.DatasetFilter
	SnapshotPrefix string
	PrunePolicy    PrunePolicy
}

func (p *Pruner) Run(ctx context.Context) {

	log := ctx.Value(contextKeyLog).(Logger)

	if p.DryRun {
		log.Printf("doing dry run")
	}

	// ZFSListSnapsFiltered --> todo can extend fsfilter or need new? Have already something per fs
	// Dedicated snapshot object? Adaptor object to FilesystemVersion?

	filesystems, err := zfs.ZFSListMapping(p.DatasetFilter)
	if err != nil {
		log.Printf("error applying filesystem filter: %s", err)
		return
	}
	if len(filesystems) <= 0 {
		log.Printf("no filesystems matching filter")
		return
	}

	for _, fs := range filesystems {

		fsversions, err := zfs.ZFSListFilesystemVersions(fs, &PrefixSnapshotFilter{p.SnapshotPrefix})
		if err != nil {
			log.Printf("error listing filesytem versions of %s: %s", fs, err)
			continue
		}
		if len(fsversions) == 0 {
			log.Printf("no filesystem versions matching prefix '%s'", p.SnapshotPrefix)
			continue
		}

		l := util.NewPrefixLogger(log, fs.ToString())

		dbgj, err := json.Marshal(fsversions)
		if err != nil {
			panic(err)
		}
		l.Printf("DEBUG: FSVERSIONS=%s", dbgj)

		keep, remove, err := p.PrunePolicy.Prune(fs, fsversions)
		if err != nil {
			l.Printf("error evaluating prune policy: %s", err)
			continue
		}

		dbgj, err = json.Marshal(keep)
		if err != nil {
			panic(err)
		}
		l.Printf("DEBUG: KEEP=%s", dbgj)

		dbgj, err = json.Marshal(remove)
		l.Printf("DEBUG: REMOVE=%s", dbgj)

		describe := func(v zfs.FilesystemVersion) string {
			timeSince := v.Creation.Sub(p.Now)
			const day time.Duration = 24 * time.Hour
			days := timeSince / day
			remainder := timeSince % day
			return fmt.Sprintf("%s@%dd%s from now", v.ToAbsPath(fs), days, remainder)
		}

		for _, v := range remove {
			l.Printf("remove %s", describe(v))
			// echo what we'll do and exec zfs destroy if not dry run
			// TODO special handling for EBUSY (zfs hold)
			// TODO error handling for clones? just echo to cli, skip over, and exit with non-zero status code (we're idempotent)
			if !p.DryRun {
				err := zfs.ZFSDestroyFilesystemVersion(fs, v)
				if err != nil {
					// handle
					l.Printf("error: %s", err)
				}
			}
		}

	}

	return

}
