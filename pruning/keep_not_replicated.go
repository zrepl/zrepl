package pruning

type KeepNotReplicated struct{}

func (*KeepNotReplicated) KeepRule(snaps []Snapshot) PruneSnapshotsResult {
	return partitionSnapList(snaps, func(snapshot Snapshot) bool {
		return snapshot.Replicated()
	})
}

func NewKeepNotReplicated() *KeepNotReplicated {
	return &KeepNotReplicated{}
}
