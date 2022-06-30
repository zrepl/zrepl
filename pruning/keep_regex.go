package pruning

import (
	"regexp"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/filters"
	"github.com/zrepl/zrepl/zfs"
)

type KeepRegex struct {
	KeepCommon
	negate bool
}

var _ KeepRule = &KeepRegex{}

func NewKeepRegex(filesystems config.FilesystemsFilter, expr string, negate bool) (*KeepRegex, error) {
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, err
	}

	fsf, err := filters.DatasetMapFilterFromConfig(filesystems)
	if err != nil {
		return nil, err
	}

	return &KeepRegex{KeepCommon{re, fsf}, negate}, nil
}

func MustKeepRegex(filesystems config.FilesystemsFilter, expr string, negate bool) *KeepRegex {
	k, err := NewKeepRegex(filesystems, expr, negate)
	if err != nil {
		panic(err)
	}
	return k
}

func (k *KeepRegex) GetFSFilter() zfs.DatasetFilter {
	return k.fsf
}

func (k *KeepRegex) KeepRule(snaps []Snapshot) []Snapshot {
	return filterSnapList(snaps, func(s Snapshot) bool {
		if k.negate {
			return k.re.FindStringIndex(s.Name()) != nil
		} else {
			return k.re.FindStringIndex(s.Name()) == nil
		}
	})
}
