package pruning

type KeepNotReplicated struct {
	forceConstructor struct{}
}

func (*KeepNotReplicated) KeepRule(snaps []Snapshot) (destroyList []Snapshot) {
	return filterSnapList(snaps, func(snapshot Snapshot) bool {
		return snapshot.Replicated()
	})
}

func NewKeepNotReplicated() *KeepNotReplicated {
	return &KeepNotReplicated{}
}
