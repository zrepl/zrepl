package cmd

import (
	"context"
	"encoding/json"
	"fmt"
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
		log.Info("doing dry run")
	}

	filesystems, err := zfs.ZFSListMapping(p.DatasetFilter)
	if err != nil {
		log.WithError(err).Error("error applying filesystem filter")
		return nil, err
	}
	if len(filesystems) <= 0 {
		log.Info("no filesystems matching filter")
		return nil, err
	}

	r = make([]PruneResult, 0, len(filesystems))

	for _, fs := range filesystems {

		log := log.WithField("filesystem", fs.ToString())

		fsversions, err := zfs.ZFSListFilesystemVersions(fs, &PrefixSnapshotFilter{p.SnapshotPrefix})
		if err != nil {
			log.WithError(err).Error("error listing filesytem versions")
			continue
		}
		if len(fsversions) == 0 {
			log.WithField("prefix", p.SnapshotPrefix).Info("no filesystem versions matching prefix")
			continue
		}

		dbgj, err := json.Marshal(fsversions)
		if err != nil {
			panic(err)
		}
		log.WithField("fsversions", string(dbgj)).Debug()

		keep, remove, err := p.PrunePolicy.Prune(fs, fsversions)
		if err != nil {
			log.WithError(err).Error("error evaluating prune policy")
			continue
		}

		dbgj, err = json.Marshal(keep)
		if err != nil {
			panic(err)
		}
		log.WithField("keep", string(dbgj)).Debug()

		dbgj, err = json.Marshal(remove)
		log.WithField("remove", string(dbgj)).Debug()

		r = append(r, PruneResult{fs, fsversions, keep, remove})

		makeFields := func(v zfs.FilesystemVersion) (fields map[string]interface{}) {
			fields = make(map[string]interface{})
			fields["version"] = v.ToAbsPath(fs)
			timeSince := v.Creation.Sub(p.Now)
			fields["age_ns"] = timeSince
			const day time.Duration = 24 * time.Hour
			days := timeSince / day
			remainder := timeSince % day
			fields["age_str"] = fmt.Sprintf("%dd%s", days, remainder)
			return
		}

		for _, v := range remove {
			fields := makeFields(v)
			log.WithFields(fields).Info("destroying version")
			// echo what we'll do and exec zfs destroy if not dry run
			// TODO special handling for EBUSY (zfs hold)
			// TODO error handling for clones? just echo to cli, skip over, and exit with non-zero status code (we're idempotent)
			if !p.DryRun {
				err := zfs.ZFSDestroyFilesystemVersion(fs, v)
				if err != nil {
					// handle
					log.WithFields(fields).WithError(err).Error("error destroying version")
				}
			}
		}

	}

	return

}
