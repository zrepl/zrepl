package zfs

import "fmt"

type DatasetFilter interface {
	Filter(p *DatasetPath) (pass bool, err error)
}

func ZFSListMapping(filter DatasetFilter) (datasets []*DatasetPath, err error) {

	if filter == nil {
		panic("filter must not be nil")
	}

	var lines [][]string
	lines, err = ZFSList([]string{"name"}, "-r", "-t", "filesystem,volume")

	datasets = make([]*DatasetPath, 0, len(lines))

	for _, line := range lines {

		var path *DatasetPath
		if path, err = NewDatasetPath(line[0]); err != nil {
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
