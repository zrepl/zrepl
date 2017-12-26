package cmd

import (
	"context"
	"fmt"
	"github.com/zrepl/zrepl/zfs"
	"time"
)

type Pruner struct {
	task           *Task
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

func (p *Pruner) filterFilesystems() (filesystems []*zfs.DatasetPath, stop bool) {
	p.task.Enter("filter_fs")
	defer p.task.Finish()
	filesystems, err := zfs.ZFSListMapping(p.DatasetFilter)
	if err != nil {
		p.task.Log().WithError(err).Error("error applying filesystem filter")
		return nil, true
	}
	if len(filesystems) <= 0 {
		p.task.Log().Info("no filesystems matching filter")
		return nil, true
	}
	return filesystems, false
}

func (p *Pruner) filterVersions(fs *zfs.DatasetPath) (fsversions []zfs.FilesystemVersion, stop bool) {
	p.task.Enter("filter_versions")
	defer p.task.Finish()
	log := p.task.Log().WithField(logFSField, fs.ToString())

	// only prune snapshots, bookmarks are kept forever
	snapshotFilter := NewTypedPrefixFilter(p.SnapshotPrefix, zfs.Snapshot)
	fsversions, err := zfs.ZFSListFilesystemVersions(fs, snapshotFilter)
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

func (p *Pruner) pruneFilesystem(fs *zfs.DatasetPath) (r PruneResult, valid bool) {
	p.task.Enter("prune_fs")
	defer p.task.Finish()
	log := p.task.Log().WithField(logFSField, fs.ToString())

	fsversions, stop := p.filterVersions(fs)
	if stop {
		return
	}

	p.task.Enter("prune_policy")
	keep, remove, err := p.PrunePolicy.Prune(fs, fsversions)
	p.task.Finish()
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
			p.task.Enter("destroy")
			err := zfs.ZFSDestroyFilesystemVersion(fs, v)
			p.task.Finish()
			if err != nil {
				log.WithFields(fields).WithError(err).Error("error destroying version")
			}
		}
	}
	return r, true
}

func (p *Pruner) Run(ctx context.Context) (r []PruneResult, err error) {
	p.task.Enter("run")
	defer p.task.Finish()

	if p.DryRun {
		p.task.Log().Info("doing dry run")
	}

	filesystems, stop := p.filterFilesystems()
	if stop {
		return
	}

	r = make([]PruneResult, 0, len(filesystems))

	for _, fs := range filesystems {
		res, ok := p.pruneFilesystem(fs)
		if ok {
			r = append(r, res)
		}
	}

	return

}
