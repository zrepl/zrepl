package pruning

import (
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/zfs"
)

type KeepRegex struct {
	KeepCommon
	negate bool
}

var _ KeepRule = &KeepRegex{}

func NewKeepRegex(in *config.PruneKeepRegex) (*KeepRegex, error) {
	kc, err := newKeepCommon(&in.PruneKeepCommon)
	if err != nil {
		return nil, err
	}

	return &KeepRegex{kc, in.Negate}, nil
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
