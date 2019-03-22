package filters

import (
	"strings"

	"github.com/zrepl/zrepl/zfs"
)

type AnyFSVFilter struct{}

func NewAnyFSVFilter() AnyFSVFilter {
	return AnyFSVFilter{}
}

var _ zfs.FilesystemVersionFilter = AnyFSVFilter{}

func (AnyFSVFilter) Filter(t zfs.VersionType, name string) (accept bool, err error) {
	return true, nil
}

type PrefixFilter struct {
	prefix    string
	fstype    zfs.VersionType
	fstypeSet bool // optionals anyone?
}

var _ zfs.FilesystemVersionFilter = &PrefixFilter{}

func NewPrefixFilter(prefix string) *PrefixFilter {
	return &PrefixFilter{prefix: prefix}
}

func NewTypedPrefixFilter(prefix string, versionType zfs.VersionType) *PrefixFilter {
	return &PrefixFilter{prefix, versionType, true}
}

func (f *PrefixFilter) Filter(t zfs.VersionType, name string) (accept bool, err error) {
	fstypeMatches := (!f.fstypeSet || t == f.fstype)
	prefixMatches := strings.HasPrefix(name, f.prefix)
	return fstypeMatches && prefixMatches, nil
}
