package zfs

type DatasetPathForest struct {
	roots []*datasetPathTree
}

func NewDatasetPathForest() *DatasetPathForest {
	return &DatasetPathForest{
		make([]*datasetPathTree, 0),
	}
}

func (f *DatasetPathForest) Add(p *DatasetPath) {
	if len(p.comps) <= 0 {
		panic("dataset path too short. must have length > 0")
	}

	// Find its root
	var root *datasetPathTree
	for _, r := range f.roots {
		if r.Add(p.comps) {
			root = r
			break
		}
	}
	if root == nil {
		root = newDatasetPathTree(p.comps)
		f.roots = append(f.roots, root)
	}
}

type DatasetPathVisit struct {
	Path *DatasetPath
	// If true, the dataset referenced by Path was not in the list of datasets to traverse
	FilledIn bool
	Parent   *DatasetPathVisit
}

type DatasetPathsVisitor func(v *DatasetPathVisit) (visitChildTree bool)

// Traverse a list of DatasetPaths top down, i.e. given a set of datasets with same
// path prefix, those with shorter prefix are traversed first.
// If there are gaps, i.e. the intermediary component a/b between a and a/b/c,
// those gaps are still visited but the FilledIn property of the visit is set to true.
func (f *DatasetPathForest) WalkTopDown(visitor DatasetPathsVisitor) {

	for _, r := range f.roots {
		r.WalkTopDown(&DatasetPathVisit{
			Path:     &DatasetPath{nil},
			FilledIn: true,
			Parent:   nil,
		}, visitor)
	}

}

/* PRIVATE IMPLEMENTATION */

type datasetPathTree struct {
	Component string
	FilledIn  bool
	Children  []*datasetPathTree
}

func (t *datasetPathTree) Add(p []string) bool {

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

func (t *datasetPathTree) WalkTopDown(parent *DatasetPathVisit, visitor DatasetPathsVisitor) {

	thisVisitPath := parent.Path.Copy()
	thisVisitPath.Extend(&DatasetPath{[]string{t.Component}})

	thisVisit := &DatasetPathVisit{
		thisVisitPath,
		t.FilledIn,
		parent,
	}
	visitChildTree := visitor(thisVisit)

	if visitChildTree {
		for _, c := range t.Children {
			c.WalkTopDown(thisVisit, visitor)
		}
	}

}

func newDatasetPathTree(initialComps []string) (t *datasetPathTree) {
	t = &datasetPathTree{}
	cur := t
	for i, comp := range initialComps {
		cur.Component = comp
		cur.FilledIn = true
		cur.Children = make([]*datasetPathTree, 0, 1)
		if i == len(initialComps)-1 {
			cur.FilledIn = false // last component is not filled in
			break
		}
		child := &datasetPathTree{}
		cur.Children = append(cur.Children, child)
		cur = child
	}
	return t
}
