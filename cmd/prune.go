package cmd

import (
	"context"
	"fmt"
	"github.com/zrepl/zrepl/util"
	"github.com/zrepl/zrepl/zfs"
	"sync"
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

// FIXME must not call p.task.Enter because it runs in parallel
func (p *Pruner) filterFilesystems() (filesystems []*zfs.DatasetPath, stop bool) {
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

// FIXME must not call p.task.Enter because it runs in parallel
func (p *Pruner) filterVersions(fs *zfs.DatasetPath) (fsversions []zfs.FilesystemVersion, stop bool) {
	log := p.task.Log().WithField(logFSField, fs.ToString())

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

// FIXME must not call p.task.Enter because it runs in parallel
func (p *Pruner) pruneFilesystem(fs *zfs.DatasetPath, destroySemaphore util.Semaphore) (r PruneResult, valid bool) {
	log := p.task.Log().WithField(logFSField, fs.ToString())

	fsversions, stop := p.filterVersions(fs)
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

	var wg sync.WaitGroup
	for v := range remove {
		wg.Add(1)
		go func(v zfs.FilesystemVersion) {
			defer wg.Done()
			// log fields
			fields := make(map[string]interface{})
			fields["version"] = v.ToAbsPath(fs)
			timeSince := v.Creation.Sub(p.Now)
			fields["age_ns"] = timeSince
			const day time.Duration = 24 * time.Hour
			days := timeSince / day
			remainder := timeSince % day
			fields["age_str"] = fmt.Sprintf("%dd%s", days, remainder)

			log.WithFields(fields).Info("destroying version")
			// echo what we'll do and exec zfs destroy if not dry run
			// TODO special handling for EBUSY (zfs hold)
			// TODO error handling for clones? just echo to cli, skip over, and exit with non-zero status code (we're idempotent)
			if !p.DryRun {
				destroySemaphore.Down()
				err := zfs.ZFSDestroyFilesystemVersion(fs, v)
				destroySemaphore.Up()
				if err != nil {
					log.WithFields(fields).WithError(err).Error("error destroying version")
				}
			}
		}(remove[v])
	}
	wg.Wait()

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

	maxConcurrentDestroy := len(filesystems)
	p.task.Log().WithField("max_concurrent_destroy", maxConcurrentDestroy).Info("begin concurrent destroy")
	destroySem := util.NewSemaphore(maxConcurrentDestroy)

	resChan := make(chan PruneResult, len(filesystems))
	var wg sync.WaitGroup
	for _, fs := range filesystems {
		wg.Add(1)
		go func(fs *zfs.DatasetPath) {
			defer wg.Done()
			res, ok := p.pruneFilesystem(fs, destroySem)
			if ok {
				resChan <- res
			}

		}(fs)
	}
	wg.Wait()
	close(resChan)

	p.task.Log().Info("destroys done")

	r = make([]PruneResult, 0, len(filesystems))
	for res := range resChan {
		r = append(r, res)
	}

	return

}
