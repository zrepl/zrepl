package pruning

import (
	"github.com/zrepl/zrepl/config"
)

func MustNewKeepGrid(filesystems config.FilesystemsFilter, regex, gridspec string) *KeepGrid {
	ris, err := config.ParseRetentionIntervalSpec(gridspec)
	if err != nil {
		panic(err)
	}

	k, err := NewKeepGrid(&config.PruneGrid{
        PruneKeepCommon: config.PruneKeepCommon{Filesystems: filesystems, Regex: regex},
        Grid:            ris,
	})
	if err != nil {
		panic(err)
	}
	return k
}

func MustKeepLastN(filesystems config.FilesystemsFilter, n int, regex string) *KeepLastN {
	k, err := NewKeepLastN(&config.PruneKeepLastN{
        PruneKeepCommon: config.PruneKeepCommon{Filesystems: filesystems, Regex: regex},
        Count:           n,
	})
	if err != nil {
		panic(err)
	}
	return k
}

func MustKeepNotReplicated(filesystems config.FilesystemsFilter) *KeepNotReplicated {
	k, err := NewKeepNotReplicated(&config.PruneKeepNotReplicated{
		PruneKeepCommon:      config.PruneKeepCommon{Filesystems: filesystems},
		KeepSnapshotAtCursor: false,
	})
	if err != nil {
		panic(err)
	}
	return k
}

func MustKeepRegex(filesystems config.FilesystemsFilter, regex string, negate bool) *KeepRegex {
	k, err := NewKeepRegex(&config.PruneKeepRegex{
        PruneKeepCommon: config.PruneKeepCommon{Filesystems: filesystems, Regex: regex},
		Negate:          negate,
	})
	if err != nil {
		panic(err)
	}
	return k
}
