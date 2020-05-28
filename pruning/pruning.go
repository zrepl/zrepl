package pruning

import (
	"fmt"
	"time"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/endpoint"
)

type KeepRule interface {
	KeepRule(snaps []Snapshot) PruneSnapshotsResult
}

type Snapshot interface {
	GetName() string
	Replicated() bool
	GetCreation() time.Time
	GetCreateTXG() uint64
	StepHolds() []StepHold
}

type StepHold interface {
	GetJobID() endpoint.JobID
}

type PruneSnapshotsResult struct {
	Remove, Keep []Snapshot
}

// The returned snapshot results are a partition of the snaps argument.
// That means than len(Remove) + len(Keep) == len(snaps)
func PruneSnapshots(snapsI []Snapshot, keepRules []KeepRule) PruneSnapshotsResult {

	if len(keepRules) == 0 {
		return PruneSnapshotsResult{Remove: nil, Keep: snapsI}
	}

	type snapshot struct {
		Snapshot
		keepCount, removeCount int
	}

	// project down to snapshot
	snaps := make([]Snapshot, len(snapsI))
	for i := range snaps {
		snaps[i] = &snapshot{snapsI[i], 0, 0}
	}

	for _, r := range keepRules {

		ruleImplCheckSet := make(map[Snapshot]int, len(snaps))
		for _, s := range snaps {
			ruleImplCheckSet[s] = ruleImplCheckSet[s] + 1
		}

		ruleResults := r.KeepRule(snaps)

		for _, s := range snaps {
			ruleImplCheckSet[s] = ruleImplCheckSet[s] - 1
		}
		for _, n := range ruleImplCheckSet {
			if n != 0 {
				panic(fmt.Sprintf("incorrect rule implementation: %T", r))
			}
		}

		for _, s := range ruleResults.Remove {
			s.(*snapshot).removeCount++
		}
		for _, s := range ruleResults.Keep {
			s.(*snapshot).keepCount++
		}
	}

	remove := make([]Snapshot, 0, len(snaps))
	keep := make([]Snapshot, 0, len(snaps))
	for _, sI := range snaps {
		s := sI.(*snapshot)
		if s.removeCount == len(keepRules) {
			// all keep rules agree to remove the snap
			remove = append(remove, s.Snapshot)
		} else {
			keep = append(keep, s.Snapshot)
		}
	}

	return PruneSnapshotsResult{Remove: remove, Keep: keep}
}

func RulesFromConfig(mainJobId endpoint.JobID, in []config.PruningEnum) (rules []KeepRule, err error) {
	rules = make([]KeepRule, len(in))
	for i := range in {
		rules[i], err = RuleFromConfig(mainJobId, in[i])
		if err != nil {
			return nil, errors.Wrapf(err, "cannot build rule #%d", i)
		}
	}
	return rules, nil
}

func RuleFromConfig(mainJobId endpoint.JobID, in config.PruningEnum) (KeepRule, error) {
	switch v := in.Ret.(type) {
	case *config.PruneKeepNotReplicated:
		return NewKeepNotReplicated(), nil
	case *config.PruneKeepLastN:
		return NewKeepLastN(v.Count)
	case *config.PruneKeepRegex:
		return NewKeepRegex(v.Regex, v.Negate)
	case *config.PruneGrid:
		return NewKeepGrid(v)
	case *config.PruneKeepStepHolds:
		return NewKeepStepHolds(mainJobId, v.AdditionalJobIds)
	default:
		return nil, fmt.Errorf("unknown keep rule type %T", v)
	}
}
