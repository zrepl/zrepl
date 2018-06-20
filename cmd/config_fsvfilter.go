package cmd

import (
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/zfs"
	"strings"
)

type PrefixFilter struct {
	prefix    string
	fstype    zfs.VersionType
	fstypeSet bool // optionals anyone?
}

func NewPrefixFilter(prefix string) *PrefixFilter {
	return &PrefixFilter{prefix: prefix}
}

func NewTypedPrefixFilter(prefix string, versionType zfs.VersionType) *PrefixFilter {
	return &PrefixFilter{prefix, versionType, true}
}

func parseSnapshotPrefix(i string) (p string, err error) {
	if len(i) <= 0 {
		err = errors.Errorf("snapshot prefix must not be empty string")
		return
	}
	p = i
	return
}

func (f *PrefixFilter) Filter(t zfs.VersionType, name string) (accept bool, err error) {
	fstypeMatches := (!f.fstypeSet || t == f.fstype)
	prefixMatches := strings.HasPrefix(name, f.prefix)
	return fstypeMatches && prefixMatches, nil
}
