package pruning

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
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
	inputs   []Snapshot
	rules    []KeepRule
	exp, eff map[string]bool
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
			tc.eff = make(map[string]bool)
			for _, s := range remove {
				tc.eff[s.Name()] = true
			}
			assert.True(t, mapEqual(tc.exp, tc.eff), fmt.Sprintf("is %v but should be %v", tc.eff, tc.exp))
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
				MustKeepRegex("foo_"),
			},
			exp: map[string]bool{
				"bar_123": true,
			},
		},
		"multipleRules": {
			inputs: inputs["s1"],
			rules: []KeepRule{
				MustKeepRegex("foo_"),
				MustKeepRegex("bar_"),
			},
			exp: map[string]bool{},
		},
		"onlyThoseRemovedByAllAreRemoved": {
			inputs: inputs["s1"],
			rules: []KeepRule{
				MustKeepRegex("notInS1"), // would remove all
				MustKeepRegex("bar_"),    // would remove all but bar_, i.e. foo_.*
			},
			exp: map[string]bool{
				"foo_123": true,
				"foo_456": true,
			},
		},
		"noRulesKeepsAll": {
			inputs: inputs["s1"],
			rules:  []KeepRule{},
			exp: map[string]bool{
				"foo_123": true,
				"foo_456": true,
				"bar_123": true,
			},
		},
		"noSnaps": {
			inputs: []Snapshot{},
			rules: []KeepRule{
				MustKeepRegex("foo_"),
			},
			exp: map[string]bool{},
		},
	}

	testTable(tcs, t)
}
