package pruning

import "testing"

func TestKeppMostRecentCommonAncestor(t *testing.T) {

	o := func(minutes int) time.Time {
		return time.Unix(123, 0).Add(time.Duration(minutes) * time.Minute)
	}

	tcs := map[string]testCase{
		"empty": {
			inputs:     []Snapshot{},
			rules:      []KeepRule{NewKeepMostRecentCommonAncestor()},
			expDestroy: map[string]bool{},
		},
		"nil": {
			inputs:     nil,
			rules:      []KeepRule{NewKeepMostRecentCommonAncestor()},
			expDestroy: map[string]bool{},
		},
		"": {
			inputs: []Snapshot{
				stubSnap{name: "s1", date: o(5), bothSides: }
			},
		}
	}

	testTable(tcs, t)

}
