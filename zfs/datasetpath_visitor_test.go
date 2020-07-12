package zfs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewDatasetPathTree(t *testing.T) {

	r := newDatasetPathTree(toDatasetPath("pool1/foo/bar").comps)

	assert.Equal(t, "pool1", r.Component)
	assert.True(t, len(r.Children) == 1)
	cr := r.Children[0]
	assert.Equal(t, "foo", cr.Component)
	assert.True(t, len(cr.Children) == 1)
	ccr := cr.Children[0]
	assert.Equal(t, "bar", ccr.Component)

}

type visitRecorder struct {
	visits []DatasetPathVisit
}

func makeVisitRecorder() (v DatasetPathsVisitor, rec *visitRecorder) {
	rec = &visitRecorder{
		visits: make([]DatasetPathVisit, 0),
	}
	v = func(v *DatasetPathVisit) bool {
		rec.visits = append(rec.visits, *v)
		return true
	}
	return
}

func buildForest(paths []*DatasetPath) (f *DatasetPathForest) {
	f = NewDatasetPathForest()
	for _, p := range paths {
		f.Add(p)
	}
	return
}

type expectedDatasetPathVisit struct {
	expectPath   *DatasetPath
	expectFillIn bool
}

func (e *expectedDatasetPathVisit) AssertEqual(t *testing.T, actual DatasetPathVisit) {
	assert.Equal(t, e.expectPath, actual.Path)
	assert.Equal(t, e.expectFillIn, actual.FilledIn)
	if actual.Parent != nil {
		assert.Equal(t, actual.Parent.Path.Length()+1, e.expectPath.Length())
		assert.True(t, e.expectPath.HasPrefix(actual.Parent.Path))
	}
}

func expectedDatasetPathVisits(t *testing.T, expected []expectedDatasetPathVisit, actual []DatasetPathVisit) {
	assert.Equal(t, len(expected), len(actual))
	for i := range expected {
		expected[i].AssertEqual(t, actual[i])
	}
}

func TestDatasetPathForestWalkTopDown(t *testing.T) {

	paths := []*DatasetPath{
		toDatasetPath("pool1"),
		toDatasetPath("pool1/foo/bar"),
		toDatasetPath("pool1/foo/bar/looloo"),
		toDatasetPath("pool2/test/bar"),
	}

	v, rec := makeVisitRecorder()

	buildForest(paths).WalkTopDown(v)

	expectedVisits := []expectedDatasetPathVisit{
		{toDatasetPath("pool1"), false},
		{toDatasetPath("pool1/foo"), true},
		{toDatasetPath("pool1/foo/bar"), false},
		{toDatasetPath("pool1/foo/bar/looloo"), false},
		{toDatasetPath("pool2"), true},
		{toDatasetPath("pool2/test"), true},
		{toDatasetPath("pool2/test/bar"), false},
	}
	expectedDatasetPathVisits(t, expectedVisits, rec.visits)
}

func TestDatasetPathWalkTopDownWorksUnordered(t *testing.T) {

	paths := []*DatasetPath{
		toDatasetPath("pool1"),
		toDatasetPath("pool1/foo/bar/looloo"),
		toDatasetPath("pool1/foo/bar"),
		toDatasetPath("pool1/bang/baz"),
	}

	v, rec := makeVisitRecorder()

	buildForest(paths).WalkTopDown(v)

	expectedVisits := []expectedDatasetPathVisit{
		{toDatasetPath("pool1"), false},
		{toDatasetPath("pool1/foo"), true},
		{toDatasetPath("pool1/foo/bar"), false},
		{toDatasetPath("pool1/foo/bar/looloo"), false},
		{toDatasetPath("pool1/bang"), true},
		{toDatasetPath("pool1/bang/baz"), false},
	}

	expectedDatasetPathVisits(t, expectedVisits, rec.visits)

}
