package cmd

import "github.com/zrepl/zrepl/zfs"

type NoPrunePolicy struct{}

func (p NoPrunePolicy) Prune(fs *zfs.DatasetPath, versions []zfs.FilesystemVersion) (keep, remove []zfs.FilesystemVersion, err error) {
	keep = versions
	remove = []zfs.FilesystemVersion{}
	return
}
