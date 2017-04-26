package zfs

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestGlobMapping(t *testing.T) {

	m := GlobMapping{
		PrefixPath: toDatasetPath("tank/usr/home"),
		TargetRoot: toDatasetPath("backups/share1"),
	}

	var r DatasetPath
	var err error

	r, err = m.Map(toDatasetPath("tank/usr/home"))
	assert.Nil(t, err)
	assert.Equal(t, toDatasetPath("backups/share1/tank/usr/home"), r)

	r, err = m.Map(toDatasetPath("zroot"))
	assert.Equal(t, NoMatchError, err, "prefix-only match is an error")

	r, err = m.Map(toDatasetPath("zroot/notmapped"))
	assert.Equal(t, NoMatchError, err, "non-prefix is an error")

}

func TestComboMapping(t *testing.T) {

	m1 := GlobMapping{
		PrefixPath: toDatasetPath("a/b"),
		TargetRoot: toDatasetPath("c/d"),
	}

	m2 := GlobMapping{
		PrefixPath: toDatasetPath("a/x"),
		TargetRoot: toDatasetPath("c/y"),
	}

	c := ComboMapping{
		Mappings: []DatasetMapping{m1, m2},
	}

	var r DatasetPath
	var err error

	p := toDatasetPath("a/b/q")

	r, err = m2.Map(p)
	assert.Equal(t, NoMatchError, err)

	r, err = c.Map(p)
	assert.Nil(t, err)
	assert.Equal(t, toDatasetPath("c/d/a/b/q"), r)

}

func TestDirectMapping(t *testing.T) {

	m := DirectMapping{
		Source: toDatasetPath("a/b/c"),
		Target: toDatasetPath("x/y/z"),
	}

	var r DatasetPath
	var err error

	r, err = m.Map(toDatasetPath("a/b/c"))
	assert.Nil(t, err)
	assert.Equal(t, m.Target, r)

	r, err = m.Map(toDatasetPath("not/matching"))
	assert.Equal(t, NoMatchError, err)

	r, err = m.Map(toDatasetPath("a/b"))
	assert.Equal(t, NoMatchError, err)

}

func TestExecMapping(t *testing.T) {

	var err error

	var m DatasetMapping
	m = NewExecMapping("test_helpers/exec_mapping_good.sh", "nostop")
	assert.NoError(t, err)

	var p DatasetPath
	p, err = m.Map(toDatasetPath("nomap/foobar"))

	assert.Equal(t, NoMatchError, err)

	p, err = m.Map(toDatasetPath("willmap/something"))
	assert.Nil(t, err)
	assert.Equal(t, toDatasetPath("didmap/willmap/something"), p)

}
