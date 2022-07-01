package pruning

import (
	"testing"
)

func TestNewKeepNotReplicated(t *testing.T) {

	inputs := map[string][]Snapshot{
		"s1": {
			stubSnap{name: "1", replicated: true},
			stubSnap{name: "2", replicated: false},
			stubSnap{name: "3", replicated: true},
		},
		"s2": {},
	}

	tcs := map[string]testCase{
		"destroysOnlyReplicated": {
			inputs: inputs["s1"],
			rules: []KeepRule{
				MustKeepNotReplicated(map[string]bool{}),
			},
			expDestroy: map[string]bool{
				"1": true, "3": true,
			},
		},
		"empty": {
			inputs: inputs["s2"],
			rules: []KeepRule{
				MustKeepNotReplicated(map[string]bool{}),
			},
			expDestroy: map[string]bool{},
		},
	}

	testTable(tcs, t)

}
