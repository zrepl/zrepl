package pruning

import (
	"testing"
	"time"
)

type stubSnap struct {
	name       string
	replicated bool
	date       time.Time
	bothSides bool
}

func (s stubSnap) Name() string { return s.name }

func (s stubSnap) Replicated() bool { return s.replicated }

func (s stubSnap) Date() time.Time { return s.date }

type testCase struct {
	inputs                 []Snapshot
	rules                  []KeepRule
	expDestroy, effDestroy map[string]bool
	expDestroyAlternatives []map[string]bool
}

type snapshotList []Snapshot

func (l snapshotList) ContainsName(n string) bool {
	for _, s := range l {
		if s.Name() == n {
			return true
		}
	}
	return false
}

func (l snapshotList) NameList() []string {
	res := make([]string, len(l))
	for i, s := range l {
		res[i] = s.Name()
	}
	return res
}

func testTable(tcs map[string]testCase, t *testing.T) {
	mapEqual := func(a, b map[string]bool) bool {
		if len(a) != len(b) {
			return false
		}
		for k, v := range a {
			if w, ok := b[k]; !ok || v != w {
				return false
			}
		}
		return true
	}

	for name := range tcs {
		t.Run(name, func(t *testing.T) {
			tc := tcs[name]
			remove := PruneSnapshots(tc.inputs, tc.rules)
			tc.effDestroy = make(map[string]bool)
			for _, s := range remove {
				tc.effDestroy[s.Name()] = true
			}
			if tc.expDestroyAlternatives == nil {
				if tc.expDestroy == nil {
					panic("must specify either expDestroyAlternatives or expDestroy")
				}
				tc.expDestroyAlternatives = []map[string]bool{tc.expDestroy}
			}
			var okAlt map[string]bool = nil
			for _, alt := range tc.expDestroyAlternatives {
				t.Logf("testing possible result: %v", alt)
				if mapEqual(alt, tc.effDestroy) {
					okAlt = alt
				}
			}
			if okAlt == nil {
				t.Errorf("no alternatives matched result: %v", tc.effDestroy)
			}
		})
	}
}

func TestPruneSnapshots(t *testing.T) {

	inputs := map[string][]Snapshot{
		"s1": []Snapshot{
			stubSnap{name: "foo_123"},
			stubSnap{name: "foo_456"},
			stubSnap{name: "bar_123"},
		},
	}

	tcs := map[string]testCase{
		"simple": {
			inputs: inputs["s1"],
			rules: []KeepRule{
				MustKeepRegex("foo_", false),
			},
			expDestroy: map[string]bool{
				"bar_123": true,
			},
		},
		"multipleRules": {
			inputs: inputs["s1"],
			rules: []KeepRule{
				MustKeepRegex("foo_", false),
				MustKeepRegex("bar_", false),
			},
			expDestroy: map[string]bool{},
		},
		"onlyThoseRemovedByAllAreRemoved": {
			inputs: inputs["s1"],
			rules: []KeepRule{
				MustKeepRegex("notInS1", false), // would remove all
				MustKeepRegex("bar_", false),    // would remove all but bar_, i.e. foo_.*
			},
			expDestroy: map[string]bool{
				"foo_123": true,
				"foo_456": true,
			},
		},
		"noRulesKeepsAll": {
			inputs:     inputs["s1"],
			rules:      []KeepRule{},
			expDestroy: map[string]bool{},
		},
		"nilRulesKeepsAll": {
			inputs:     inputs["s1"],
			rules:      nil,
			expDestroy: map[string]bool{},
		},
		"noSnaps": {
			inputs: []Snapshot{},
			rules: []KeepRule{
				MustKeepRegex("foo_", false),
			},
			expDestroy: map[string]bool{},
		},
	}

	testTable(tcs, t)
}
