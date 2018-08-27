package pruning

import (
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

func TestPruneSnapshots(t *testing.T) {

	type testCase struct {
		inputs   []Snapshot
		rules    []KeepRule
		exp, eff []Snapshot
	}

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
			exp: []Snapshot{
				stubSnap{name: "bar_123"},
			},
		},
		"multipleRules": {
			inputs: inputs["s1"],
			rules: []KeepRule{
				MustKeepRegex("foo_"),
				MustKeepRegex("bar_"),
			},
			exp: []Snapshot{},
		},
		"onlyThoseRemovedByAllAreRemoved": {
			inputs: inputs["s1"],
			rules: []KeepRule{
				MustKeepRegex("notInS1"), // would remove all
				MustKeepRegex("bar_"),    // would remove all but bar_, i.e. foo_.*
			},
			exp: []Snapshot{
				stubSnap{name: "foo_123"},
				stubSnap{name: "foo_456"},
			},
		},
		"noRulesKeepsAll": {
			inputs: inputs["s1"],
			rules:  []KeepRule{},
			exp:    inputs["s1"],
		},
		"noSnaps": {
			inputs: []Snapshot{},
			rules: []KeepRule{
				MustKeepRegex("foo_"),
			},
			exp: []Snapshot{},
		},
	}

	for name := range tcs {
		t.Run(name, func(t *testing.T) {
			tc := tcs[name]
			tc.eff = PruneSnapshots(tc.inputs, tc.rules)
			assert.Equal(t, tc.exp, tc.eff)
		})
	}

}
