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

type PruneResult struct {
	Filesystem *zfs.DatasetPath
	All        []zfs.FilesystemVersion
	Keep       []zfs.FilesystemVersion
	Remove     []zfs.FilesystemVersion
}

func (p *Pruner) Run(ctx context.Context) (r []PruneResult, err error) {

	log := ctx.Value(contextKeyLog).(Logger)

	if p.DryRun {
		log.Printf("doing dry run")
	}

	filesystems, err := zfs.ZFSListMapping(p.DatasetFilter)
	if err != nil {
		log.Printf("error applying filesystem filter: %s", err)
		return nil, err
	}
	if len(filesystems) <= 0 {
		log.Printf("no filesystems matching filter")
		return nil, err
	}

	r = make([]PruneResult, 0, len(filesystems))

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

		r = append(r, PruneResult{fs, fsversions, keep, remove})

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
