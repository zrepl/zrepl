package pruning

import (
	"time"
)

type KeepRule interface {
	KeepRule(snaps []Snapshot) []Snapshot
}

type Snapshot interface {
	Name() string
	Replicated() bool
	Date() time.Time
}

func PruneSnapshots(snaps []Snapshot, keepRules []KeepRule) []Snapshot {

	if len(keepRules) == 0 {
		return snaps
	}

	remCount := make(map[Snapshot]int, len(snaps))
	for _, r := range keepRules {
		ruleRems := r.KeepRule(snaps)
		for _, ruleRem := range ruleRems {
			remCount[ruleRem]++
		}
	}

	remove := make([]Snapshot, 0, len(snaps))
	for snap, rc := range remCount {
		if rc == len(keepRules) {
			remove = append(remove, snap)
		}
	}

	return remove
}
