package pruning

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKeepRegexNegation(t *testing.T) {

	noneg := MustKeepRegex(map[string]bool{}, "^zrepl_", false)
	neg := MustKeepRegex(map[string]bool{}, "^zrepl_", true)

	snaps := []Snapshot{
		stubSnap{name: "zrepl_foobar"},
		stubSnap{name: "zrepl"},
		stubSnap{name: "barfoo"},
	}

	destroyNonNeg := snapshotList(noneg.KeepRule(snaps))
	t.Logf("non-negated rule destroys: %#v", destroyNonNeg.NameList())
	assert.True(t, destroyNonNeg.ContainsName("zrepl"))
	assert.True(t, destroyNonNeg.ContainsName("barfoo"))
	assert.False(t, destroyNonNeg.ContainsName("zrepl_foobar"))

	destroyNeg := snapshotList(neg.KeepRule(snaps))
	t.Logf("negated rule destroys: %#v", destroyNeg.NameList())
	assert.False(t, destroyNeg.ContainsName("zrepl"))
	assert.False(t, destroyNeg.ContainsName("barfoo"))
	assert.True(t, destroyNeg.ContainsName("zrepl_foobar"))

}
