package zfs

import (
	"context"
	"fmt"
)

type DatasetFilter interface {
	Filter(p *DatasetPath) (pass bool, err error)
}

func ZFSListMapping(filter DatasetFilter) (datasets []*DatasetPath, err error) {

	if filter == nil {
		panic("filter must not be nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rchan := make(chan ZFSListResult)
	go ZFSListChan(ctx, rchan, []string{"name"}, "-r", "-t", "filesystem,volume")

	datasets = make([]*DatasetPath, 0)
	for r := range rchan {

		if r.err != nil {
			err = r.err
			return
		}

		var path *DatasetPath
		if path, err = NewDatasetPath(r.fields[0]); err != nil {
			return
		}

		pass, filterErr := filter.Filter(path)
		if filterErr != nil {
			return nil, fmt.Errorf("error calling filter: %s", filterErr)
		}
		if pass {
			datasets = append(datasets, path)
		}

	}

	return
}
