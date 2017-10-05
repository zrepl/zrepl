package cmd

import (
	"fmt"
	"strings"

	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/zfs"
)

type DatasetMapFilter struct {
	entries []datasetMapFilterEntry

	// if set, only valid filter entries can be added using Add()
	// and Map() will always return an error
	filterMode bool
}

type datasetMapFilterEntry struct {
	path *zfs.DatasetPath
	// the mapping. since this datastructure acts as both mapping and filter
	// we have to convert it to the desired rep dynamically
	mapping      string
	subtreeMatch bool
}

func NewDatasetMapFilter(capacity int, filterMode bool) *DatasetMapFilter {
	return &DatasetMapFilter{
		entries:    make([]datasetMapFilterEntry, 0, capacity),
		filterMode: filterMode,
	}
}

func (m *DatasetMapFilter) Add(pathPattern, mapping string) (err error) {

	if m.filterMode {
		if _, err = m.parseDatasetFilterResult(mapping); err != nil {
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

	if m.filterMode {
		err = fmt.Errorf("using a filter for mapping simply does not work")
		return
	}

	mi, hasMapping := m.mostSpecificPrefixMapping(source)
	if !hasMapping {
		return nil, nil
	}
	me := m.entries[mi]

	if strings.HasPrefix("!", me.mapping) {
		// reject mapping
		return nil, nil
	}

	target, err = zfs.NewDatasetPath(me.mapping)
	if err != nil {
		err = fmt.Errorf("mapping target is not a dataset path: %s", err)
		return
	}
	if me.subtreeMatch {
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

	if !m.filterMode {
		err = fmt.Errorf("using a mapping as a filter does not work")
		return
	}

	mi, hasMapping := m.mostSpecificPrefixMapping(p)
	if !hasMapping {
		pass = false
		return
	}
	me := m.entries[mi]
	pass, err = m.parseDatasetFilterResult(me.mapping)
	return
}

// Construct a new filter-only DatasetMapFilter from a mapping
// The new filter allows excactly those paths that were not forbidden by the mapping.
func (m DatasetMapFilter) InvertedFilter() (inv *DatasetMapFilter, err error) {

	if m.filterMode {
		err = errors.Errorf("can only invert mappings")
		return
	}

	inv = &DatasetMapFilter{
		make([]datasetMapFilterEntry, len(m.entries)),
		true,
	}

	for i, e := range m.entries {
		inv.entries[i].path, err = zfs.NewDatasetPath(e.mapping)
		if err != nil {
			err = errors.Wrapf(err, "mapping cannot be inverted: '%s' is not a dataset path: %s", e.mapping)
			return
		}
		inv.entries[i].mapping = MapFilterResultOk
		inv.entries[i].subtreeMatch = e.subtreeMatch
	}

	return inv, nil
}

// Creates a new DatasetMapFilter in filter mode from a mapping
// All accepting mapping results are mapped to accepting filter results
// All rejecting mapping results are mapped to rejecting filter results
func (m DatasetMapFilter) AsFilter() (f *DatasetMapFilter) {

	f = &DatasetMapFilter{
		make([]datasetMapFilterEntry, len(m.entries)),
		true,
	}

	for i, e := range m.entries {
		var newe datasetMapFilterEntry = e
		if strings.HasPrefix(newe.mapping, "!") {
			newe.mapping = MapFilterResultOmit
		} else {
			newe.mapping = MapFilterResultOk
		}
		f.entries[i] = newe
	}

	return f
}

const (
	MapFilterResultOk   string = "ok"
	MapFilterResultOmit string = "!"
)

// Parse a dataset filter result
func (m DatasetMapFilter) parseDatasetFilterResult(result string) (pass bool, err error) {
	l := strings.ToLower(result)
	if l == MapFilterResultOk {
		return true, nil
	}
	if l == MapFilterResultOmit {
		return false, nil
	}
	return false, fmt.Errorf("'%s' is not a valid filter result", result)
}

func parseDatasetMapFilter(mi interface{}, filterMode bool) (f *DatasetMapFilter, err error) {

	var m map[string]string
	if err = mapstructure.Decode(mi, &m); err != nil {
		err = fmt.Errorf("maps / filters must be specified as map[string]string: %s", err)
		return
	}

	f = NewDatasetMapFilter(len(m), filterMode)
	for pathPattern, mapping := range m {
		if err = f.Add(pathPattern, mapping); err != nil {
			err = fmt.Errorf("invalid mapping entry ['%s':'%s']: %s", pathPattern, mapping, err)
			return
		}
	}
	return
}
