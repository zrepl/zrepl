package zfs

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestNewDatasetPathTree(t *testing.T) {

	r := newDatasetPathTree(toDatasetPath("pool1/foo/bar"))

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
	v = func(v DatasetPathVisit) error {
		rec.visits = append(rec.visits, v)
		return nil
	}
	return
}

func buildForest(paths []DatasetPath) (f *DatasetPathForest) {
	f = NewDatasetPathForest()
	for _, p := range paths {
		f.Add(p)
	}
	return
}

func TestDatasetPathForestWalkTopDown(t *testing.T) {

	paths := []DatasetPath{
		toDatasetPath("pool1"),
		toDatasetPath("pool1/foo/bar"),
		toDatasetPath("pool1/foo/bar/looloo"),
		toDatasetPath("pool2/test/bar"),
	}

	v, rec := makeVisitRecorder()

	buildForest(paths).WalkTopDown(v)

	expectedVisists := []DatasetPathVisit{
		{toDatasetPath("pool1"), false},
		{toDatasetPath("pool1/foo"), true},
		{toDatasetPath("pool1/foo/bar"), false},
		{toDatasetPath("pool1/foo/bar/looloo"), false},
		{toDatasetPath("pool2"), true},
		{toDatasetPath("pool2/test"), true},
		{toDatasetPath("pool2/test/bar"), false},
	}
	assert.Equal(t, expectedVisists, rec.visits)

}

func TestDatasetPathWalkTopDownWorksUnordered(t *testing.T) {

	paths := []DatasetPath{
		toDatasetPath("pool1"),
		toDatasetPath("pool1/foo/bar/looloo"),
		toDatasetPath("pool1/foo/bar"),
		toDatasetPath("pool1/bang/baz"),
	}

	v, rec := makeVisitRecorder()

	buildForest(paths).WalkTopDown(v)

	expectedVisists := []DatasetPathVisit{
		{toDatasetPath("pool1"), false},
		{toDatasetPath("pool1/foo"), true},
		{toDatasetPath("pool1/foo/bar"), false},
		{toDatasetPath("pool1/foo/bar/looloo"), false},
		{toDatasetPath("pool1/bang"), true},
		{toDatasetPath("pool1/bang/baz"), false},
	}

	assert.Equal(t, expectedVisists, rec.visits)

}
