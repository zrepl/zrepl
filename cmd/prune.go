package cmd

import (
	"context"
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

func (p *Pruner) filterFilesystems(ctx context.Context) (filesystems []*zfs.DatasetPath, stop bool) {
	filesystems, err := zfs.ZFSListMapping(p.DatasetFilter)
	if err != nil {
		getLogger(ctx).WithError(err).Error("error applying filesystem filter")
		return nil, true
	}
	if len(filesystems) <= 0 {
		getLogger(ctx).Info("no filesystems matching filter")
		return nil, true
	}
	return filesystems, false
}

func (p *Pruner) filterVersions(ctx context.Context, fs *zfs.DatasetPath) (fsversions []zfs.FilesystemVersion, stop bool) {
	log := getLogger(ctx).WithField("fs", fs.ToString())

	filter := NewPrefixFilter(p.SnapshotPrefix)
	fsversions, err := zfs.ZFSListFilesystemVersions(fs, filter)
	if err != nil {
		log.WithError(err).Error("error listing filesytem versions")
		return nil, true
	}
	if len(fsversions) == 0 {
		log.WithField("prefix", p.SnapshotPrefix).Info("no filesystem versions matching prefix")
		return nil, true
	}
	return fsversions, false
}

func (p *Pruner) pruneFilesystem(ctx context.Context, fs *zfs.DatasetPath) (r PruneResult, valid bool) {
	log := getLogger(ctx).WithField("fs", fs.ToString())

	fsversions, stop := p.filterVersions(ctx, fs)
	if stop {
		return
	}

	keep, remove, err := p.PrunePolicy.Prune(fs, fsversions)
	if err != nil {
		log.WithError(err).Error("error evaluating prune policy")
		return
	}

	log.WithField("fsversions", fsversions).
		WithField("keep", keep).
		WithField("remove", remove).
		Debug("prune policy debug dump")

	r = PruneResult{fs, fsversions, keep, remove}

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
				log.WithFields(fields).WithError(err).Error("error destroying version")
			}
		}
	}
	return r, true
}

func (p *Pruner) Run(ctx context.Context) (r []PruneResult, err error) {
	if p.DryRun {
		getLogger(ctx).Info("doing dry run")
	}

	filesystems, stop := p.filterFilesystems(ctx)
	if stop {
		return
	}

	r = make([]PruneResult, 0, len(filesystems))

	for _, fs := range filesystems {
		res, ok := p.pruneFilesystem(ctx, fs)
		if ok {
			r = append(r, res)
		}
	}

	return

}
