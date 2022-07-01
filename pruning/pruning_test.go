package pruning

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubSnap struct {
	name       string
	replicated bool
	date       time.Time
}

func (s stubSnap) Name() string { return s.name }

func (s stubSnap) Replicated() bool { return s.replicated }

func (s stubSnap) Date() time.Time { return s.date }

type testCase struct {
	inputs     []Snapshot
	rules      []KeepRule
	expDestroy map[string]bool
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
	for name := range tcs {
		t.Run(name, func(t *testing.T) {
			tc := tcs[name]
			destroyList := PruneSnapshots(tc.inputs, tc.rules)
			destroySet := make(map[string]bool, len(destroyList))
			for _, s := range destroyList {
				destroySet[s.Name()] = true
			}
			t.Logf("destroySet:\n%#v", destroySet)
			t.Logf("expected:\n%#v", tc.expDestroy)

			require.Equal(t, len(tc.expDestroy), len(destroySet))
			for name := range destroySet {
				assert.True(t, tc.expDestroy[name], "%q", name)
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

	reltime := func(secs int64) time.Time {
		return time.Unix(secs, 0)
	}

	tcs := map[string]testCase{
		"simple": {
			inputs: inputs["s1"],
			rules: []KeepRule{
				MustKeepRegex(map[string]bool{}, "foo_", false),
			},
			expDestroy: map[string]bool{
				"bar_123": true,
			},
		},
		"multipleRules": {
			inputs: inputs["s1"],
			rules: []KeepRule{
				MustKeepRegex(map[string]bool{}, "foo_", false),
				MustKeepRegex(map[string]bool{}, "bar_", false),
			},
			expDestroy: map[string]bool{},
		},
		"onlyThoseRemovedByAllAreRemoved": {
			inputs: inputs["s1"],
			rules: []KeepRule{
				MustKeepRegex(map[string]bool{}, "notInS1", false), // would remove all
				MustKeepRegex(map[string]bool{}, "bar_", false),    // would remove all but bar_, i.e. foo_.*
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
				MustKeepRegex(map[string]bool{}, "foo_", false),
			},
			expDestroy: map[string]bool{},
		},
		"multiple_grids_with_disjoint_regexes": {
			inputs: []Snapshot{
				stubSnap{"p1_a", false, reltime(4)},
				stubSnap{"p2_a", false, reltime(5)},
				stubSnap{"p1_b", false, reltime(14)},
				stubSnap{"p2_b", false, reltime(15)},
				stubSnap{"p1_c", false, reltime(29)},
				stubSnap{"p2_c", false, reltime(30)},
			},
			rules: []KeepRule{
				MustKeepGrid(map[string]bool{}, "^p1_", `1x10s | 1x10s`),
				MustKeepGrid(map[string]bool{}, "^p2_", `1x10s | 1x10s`),
			},
			expDestroy: map[string]bool{
				"p1_a": true,
				"p2_a": true,
			},
		},
	}

	testTable(tcs, t)
}
