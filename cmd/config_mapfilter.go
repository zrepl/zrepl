package cmd

import (
	"fmt"
	"strings"

	"github.com/zrepl/zrepl/zfs"
)

type DatasetMapFilter struct {
	entries []datasetMapFilterEntry

	// if set, only valid filter entries can be added using Add()
	// and Map() will always return an error
	filterOnly bool
}

type datasetMapFilterEntry struct {
	path *zfs.DatasetPath
	// the mapping. since this datastructure acts as both mapping and filter
	// we have to convert it to the desired rep dynamically
	mapping      string
	subtreeMatch bool
}

func NewDatasetMapFilter(capacity int, filterOnly bool) DatasetMapFilter {
	return DatasetMapFilter{
		entries:    make([]datasetMapFilterEntry, 0, capacity),
		filterOnly: filterOnly,
	}
}

func (m *DatasetMapFilter) Add(pathPattern, mapping string) (err error) {

	if m.filterOnly {
		if _, err = parseDatasetFilterResult(mapping); err != nil {
			return
		}
	}

	// assert path glob adheres to spec
	const SUBTREE_PATTERN string = "<"
	patternCount := strings.Count(pathPattern, SUBTREE_PATTERN)
	switch {
	case patternCount > 1:
	case patternCount == 1 && !strings.HasSuffix(pathPattern, SUBTREE_PATTERN):
		err = fmt.Errorf("pattern invalid: only one '<' at end of string allowed")
		return
	}

	pathStr := strings.TrimSuffix(pathPattern, SUBTREE_PATTERN)
	path, err := zfs.NewDatasetPath(pathStr)
	if err != nil {
		return fmt.Errorf("pattern is not a dataset path: %s", err)
	}

	entry := datasetMapFilterEntry{
		path:         path,
		mapping:      mapping,
		subtreeMatch: patternCount > 0,
	}
	m.entries = append(m.entries, entry)
	return

}

// find the most specific prefix mapping we have
//
// longer prefix wins over shorter prefix, direct wins over glob
func (m DatasetMapFilter) mostSpecificPrefixMapping(path *zfs.DatasetPath) (idx int, found bool) {
	lcp, lcp_entry_idx := -1, -1
	direct_idx := -1
	for e := range m.entries {
		entry := m.entries[e]
		ep := m.entries[e].path
		lep := ep.Length()

		switch {
		case !entry.subtreeMatch && ep.Equal(path):
			direct_idx = e
			continue
		case entry.subtreeMatch && path.HasPrefix(ep) && lep > lcp:
			lcp = lep
			lcp_entry_idx = e
		default:
			continue
		}
	}

	if lcp_entry_idx >= 0 || direct_idx >= 0 {
		found = true
		switch {
		case direct_idx >= 0:
			idx = direct_idx
		case lcp_entry_idx >= 0:
			idx = lcp_entry_idx
		}
	}
	return
}

func (m DatasetMapFilter) Map(source *zfs.DatasetPath) (target *zfs.DatasetPath, err error) {

	if m.filterOnly {
		err = fmt.Errorf("using a filter for mapping simply does not work")
		return
	}

	mi, hasMapping := m.mostSpecificPrefixMapping(source)
	if !hasMapping {
		return nil, nil
		return
	}
	me := m.entries[mi]

	target, err = zfs.NewDatasetPath(me.mapping)
	if err != nil {
		err = fmt.Errorf("mapping target is not a dataset path: %s", err)
		return
	}
	if m.entries[mi].subtreeMatch {
		// strip common prefix
		extendComps := source.Copy()
		if me.path.Empty() {
			// special case: trying to map the root => strip first component
			extendComps.TrimNPrefixComps(1)
		} else {
			extendComps.TrimPrefix(me.path)
		}
		target.Extend(extendComps)
	}
	return
}

func (m DatasetMapFilter) Filter(p *zfs.DatasetPath) (pass bool, err error) {
	mi, hasMapping := m.mostSpecificPrefixMapping(p)
	if !hasMapping {
		pass = false
		return
	}
	me := m.entries[mi]
	pass, err = parseDatasetFilterResult(me.mapping)
	return
}

// Parse a dataset filter result
func parseDatasetFilterResult(result string) (pass bool, err error) {
	l := strings.ToLower(result)
	switch strings.ToLower(l) {
	case "ok":
		pass = true
		return
	case "omit":
		return
	default:
		err = fmt.Errorf("'%s' is not a valid filter result", result)
		return
	}
	return
}
