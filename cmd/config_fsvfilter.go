package cmd

import (
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/zfs"
	"strings"
)

type PrefixSnapshotFilter struct {
	Prefix string
}

func parseSnapshotPrefix(i string) (p string, err error) {
	if len(i) <= 0 {
		err = errors.Errorf("snapshot prefix must not be empty string")
		return
	}
	p = i
	return
}

func parsePrefixSnapshotFilter(i string) (f *PrefixSnapshotFilter, err error) {
	if !(len(i) > 0) {
		err = errors.Errorf("snapshot prefix must be longer than 0 characters")
		return
	}
	f = &PrefixSnapshotFilter{i}
	return
}

func (f *PrefixSnapshotFilter) Filter(fsv zfs.FilesystemVersion) (accept bool, err error) {
	return fsv.Type == zfs.Snapshot && strings.HasPrefix(fsv.Name, f.Prefix), nil
}
