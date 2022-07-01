package pruning

import (
	"github.com/zrepl/zrepl/config"
)

func MustKeepGrid(filesystems config.FilesystemsFilter, regex, gridspec string) *KeepGrid {
	ris, err := config.ParseRetentionIntervalSpec(gridspec)
	if err != nil {
		panic(err)
	}

	k, err := NewKeepGrid(&config.KeepGrid{
		KeepCommon: config.KeepCommon{Filesystems: filesystems, Negate: false, Regex: regex},
		Grid:       ris,
	})
	if err != nil {
		panic(err)
	}
	return k
}

func MustKeepLastN(filesystems config.FilesystemsFilter, n int, regex string) *KeepLastN {
	k, err := NewKeepLastN(&config.KeepLastN{
		KeepCommon: config.KeepCommon{Filesystems: filesystems, Negate: false, Regex: regex},
		Count:      n,
	})
	if err != nil {
		panic(err)
	}
	return k
}

func MustKeepNotReplicated(filesystems config.FilesystemsFilter) *KeepNotReplicated {
	k, err := NewKeepNotReplicated(&config.KeepNotReplicated{
		KeepCommon:           config.KeepCommon{Filesystems: filesystems},
		KeepSnapshotAtCursor: false,
	})
	if err != nil {
		panic(err)
	}
	return k
}

func MustKeepRegex(filesystems config.FilesystemsFilter, regex string, negate bool) *KeepRegex {
	k, err := NewKeepRegex(&config.KeepRegex{
		KeepCommon: config.KeepCommon{Filesystems: filesystems, Negate: negate, Regex: regex},
	})
	if err != nil {
		panic(err)
	}
	return k
}
