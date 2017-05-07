package zfs

type DatasetPathForest struct {
	roots []*datasetPathTree
}

func NewDatasetPathForest() *DatasetPathForest {
	return &DatasetPathForest{
		make([]*datasetPathTree, 0),
	}
}

func (f *DatasetPathForest) Add(p DatasetPath) {
	if len(p) <= 0 {
		panic("dataset path too short. must have length > 0")
	}

	// Find its root
	var root *datasetPathTree
	for _, r := range f.roots {
		if r.Add(p) {
			root = r
			break
		}
	}
	if root == nil {
		root = newDatasetPathTree(p)
		f.roots = append(f.roots, root)
	}
}

type DatasetPathVisit struct {
	Path DatasetPath
	// If true, the dataset referenced by Path was not in the list of datasets to traverse
	FilledIn bool
}

type DatasetPathsVisitor func(v DatasetPathVisit) (visitChildTree bool)

// Traverse a list of DatasetPaths top down, i.e. given a set of datasets with same
// path prefix, those with shorter prefix are traversed first.
// If there are gaps, i.e. the intermediary component a/b bewtween a and a/b/c,
// those gaps are still visited but the FilledIn property of the visit is set to true.
func (f *DatasetPathForest) WalkTopDown(visitor DatasetPathsVisitor) {

	for _, r := range f.roots {
		r.WalkTopDown([]string{}, visitor)
	}

}

/* PRIVATE IMPLEMENTATION */

type datasetPathTree struct {
	Component string
	FilledIn  bool
	Children  []*datasetPathTree
}

func (t *datasetPathTree) Add(p DatasetPath) bool {

	if len(p) == 0 {
		return true
	}

	if p[0] == t.Component {

		remainder := p[1:]
		if len(remainder) == 0 {
			t.FilledIn = false
			return true
		}

		for _, c := range t.Children {
			if c.Add(remainder) {
				return true
			}
		}

		t.Children = append(t.Children, newDatasetPathTree(remainder))
		return true

	} else {

		return false

	}

}

func (t *datasetPathTree) WalkTopDown(parent DatasetPath, visitor DatasetPathsVisitor) {

	this := append(parent, t.Component)

	visitChildTree := visitor(DatasetPathVisit{this, t.FilledIn})

	if visitChildTree {
		for _, c := range t.Children {
			c.WalkTopDown(this, visitor)
		}
	}

}

func newDatasetPathTree(initial DatasetPath) (t *datasetPathTree) {
	t = &datasetPathTree{}
	var cur *datasetPathTree
	cur = t
	for i, comp := range initial {
		cur.Component = comp
		cur.FilledIn = true
		cur.Children = make([]*datasetPathTree, 0, 1)
		if i == len(initial)-1 {
			cur.FilledIn = false // last component is not filled in
			break
		}
		child := &datasetPathTree{}
		cur.Children = append(cur.Children, child)
		cur = child
	}
	return t
}
