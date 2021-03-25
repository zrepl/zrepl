package pruning

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKeepLastN(t *testing.T) {

	o := func(minutes int) time.Time {
		return time.Unix(123, 0).Add(time.Duration(minutes) * time.Minute)
	}

	inputs := map[string][]Snapshot{
		"s1": []Snapshot{
			stubSnap{name: "1", date: o(10)},
			stubSnap{name: "2", date: o(20)},
			stubSnap{name: "3", date: o(15)},
			stubSnap{name: "4", date: o(30)},
			stubSnap{name: "5", date: o(30)},
		},
		"s2": []Snapshot{},
	}

	tcs := map[string]testCase{
		"keep2": {
			inputs: inputs["s1"],
			rules: []KeepRule{
				MustKeepLastN(2, ""),
			},
			expDestroy: map[string]bool{
				"1": true, "2": true, "3": true,
			},
		},
		"keep1OfTwoWithSameTime": { // Keep one of two with same time
			inputs: inputs["s1"],
			rules: []KeepRule{
				MustKeepLastN(1, ""),
			},
			expDestroy: map[string]bool{"1": true, "2": true, "3": true, "4": true},
		},
		"keepMany": {
			inputs: inputs["s1"],
			rules: []KeepRule{
				MustKeepLastN(100, ""),
			},
			expDestroy: map[string]bool{},
		},
		"empty_input": {
			inputs: inputs["s2"],
			rules: []KeepRule{
				MustKeepLastN(100, ""),
			},
			expDestroy: map[string]bool{},
		},
		"empty_regex": {
			inputs: inputs["s1"],
			rules: []KeepRule{
				MustKeepLastN(4, ""),
			},
			expDestroy: map[string]bool{
				"1": true,
			},
		},
		"multiple_regexes": {
			inputs: []Snapshot{
				stubSnap{"a1", false, o(10)},
				stubSnap{"b1", false, o(11)},
				stubSnap{"a2", false, o(20)},
				stubSnap{"b2", false, o(21)},
				stubSnap{"a3", false, o(30)},
				stubSnap{"b3", false, o(31)},
			},
			rules: []KeepRule{
				MustKeepLastN(2, "^a"),
				MustKeepLastN(2, "^b"),
			},
			expDestroy: map[string]bool{
				"a1": true,
				"b1": true,
			},
		},
		"keep_more_than_matching": {
			inputs: []Snapshot{
				stubSnap{"a1", false, o(10)},
				stubSnap{"b1", false, o(11)},
				stubSnap{"a2", false, o(12)},
			},
			rules: []KeepRule{
				MustKeepLastN(3, "a"),
			},
			expDestroy: map[string]bool{
				"b1": true,
			},
		},
	}

	testTable(tcs, t)

	t.Run("mustBePositive", func(t *testing.T) {
		var err error
		_, err = NewKeepLastN(0, "foo")
		assert.Error(t, err)
		_, err = NewKeepLastN(-5, "foo")
		assert.Error(t, err)
	})

	t.Run("emptyRegexAllowed", func(t *testing.T) {
		_, err := NewKeepLastN(23, "")
		require.NoError(t, err)
	})

}
