package filters

import "github.com/zrepl/zrepl/zfs"

type AnyFSVFilter struct{}

func NewAnyFSVFilter() AnyFSVFilter {
	return AnyFSVFilter{}
}

var _ zfs.FilesystemVersionFilter = AnyFSVFilter{}

func (AnyFSVFilter) Filter(t zfs.VersionType, name string) (accept bool, err error) {
	return true, nil
}
