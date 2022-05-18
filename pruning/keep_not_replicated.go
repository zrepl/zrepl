package pruning

type KeepNotReplicated struct{}

func (*KeepNotReplicated) KeepRule(snaps []Snapshot) (destroyList []Snapshot) {
	return filterSnapList(snaps, func(snapshot Snapshot) bool {
		return snapshot.Replicated()
	})
}

func NewKeepNotReplicated() *KeepNotReplicated {
	return &KeepNotReplicated{}
}

// TODO: Add fsre to not_replicated
func (k KeepNotReplicated) MatchFS(fsPath string) (bool, error) {
	return true, nil
}
