package pruning

import (
	"fmt"
	"time"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/config"
)

type KeepRule interface {
	KeepRule(snaps []Snapshot) (destroyList []Snapshot)
}

type Snapshot interface {
	Name() string
	Replicated() bool
	Date() time.Time
	PresentOnBothSides() bool
}

// The returned snapshot list is guaranteed to only contains elements of input parameter snaps
func PruneSnapshots(snaps []Snapshot, keepRules []KeepRule) []Snapshot {

	if len(keepRules) == 0 {
		return []Snapshot{}
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

func RulesFromConfig(in []config.PruningEnum) (rules []KeepRule, err error) {
	rules = make([]KeepRule, len(in))
	for i := range in {
		rules[i], err = RuleFromConfig(in[i])
		if err != nil {
			return nil, errors.Wrapf(err, "cannot build rule #%d", i)
		}
	}
	return rules, nil
}

func RuleFromConfig(in config.PruningEnum) (KeepRule, error) {
	switch v := in.Ret.(type) {
	case *config.PruneKeepNotReplicated:
		return NewKeepNotReplicated(), nil
	case *config.PruneKeepLastN:
		return NewKeepLastN(v.Count)
	case *config.PruneKeepRegex:
		return NewKeepRegex(v.Regex, v.Negate)
	case *config.PruneGrid:
		return NewKeepGrid(v)
	default:
		return nil, fmt.Errorf("unknown keep rule type %T", v)
	}
}
