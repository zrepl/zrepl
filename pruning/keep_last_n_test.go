package pruning

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
				KeepLastN{2},
			},
			expDestroy: map[string]bool{
				"1": true, "2": true, "3": true,
			},
		},
		"keep1OfTwoWithSameTime": { // Keep one of two with same time
			inputs: inputs["s1"],
			rules: []KeepRule{
				KeepLastN{1},
			},
			expDestroy: map[string]bool{"1": true, "2": true, "3": true, "4": true},
		},
		"keepMany": {
			inputs: inputs["s1"],
			rules: []KeepRule{
				KeepLastN{100},
			},
			expDestroy: map[string]bool{},
		},
		"empty": {
			inputs: inputs["s2"],
			rules: []KeepRule{
				KeepLastN{100},
			},
			expDestroy: map[string]bool{},
		},
	}

	testTable(tcs, t)

	t.Run("mustBePositive", func(t *testing.T) {
		var err error
		_, err = NewKeepLastN(0)
		assert.Error(t, err)
		_, err = NewKeepLastN(-5)
		assert.Error(t, err)
	})

}
