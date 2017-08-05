package cmd

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/zrepl/zrepl/util"
	"github.com/zrepl/zrepl/zfs"
)

func TestSampleConfigFileIsParsedWithoutErrors(t *testing.T) {
	_, err := ParseConfig("./sampleconf/zrepl.yml")
	assert.Nil(t, err)
}

func TestParseRetentionGridStringParsing(t *testing.T) {

	intervals, err := parseRetentionGridIntervalsString("2x10m(keep=2) | 1x1h | 3x1w")

	assert.Nil(t, err)
	assert.Len(t, intervals, 6)
	proto := util.RetentionInterval{
		KeepCount: 2,
		Length:    10 * time.Minute,
	}
	assert.EqualValues(t, proto, intervals[0])
	assert.EqualValues(t, proto, intervals[1])

	proto.KeepCount = 1
	proto.Length = 1 * time.Hour
	assert.EqualValues(t, proto, intervals[2])

	proto.Length = 7 * 24 * time.Hour
	assert.EqualValues(t, proto, intervals[3])
	assert.EqualValues(t, proto, intervals[4])
	assert.EqualValues(t, proto, intervals[5])

	intervals, err = parseRetentionGridIntervalsString("|")
	assert.Error(t, err)
	intervals, err = parseRetentionGridIntervalsString("2x10m")
	assert.NoError(t, err)

	intervals, err = parseRetentionGridIntervalsString("1x10m(keep=all)")
	assert.NoError(t, err)
	assert.Len(t, intervals, 1)
	assert.EqualValues(t, util.RetentionGridKeepCountAll, intervals[0].KeepCount)

}

func TestDatasetMapFilter(t *testing.T) {

	expectMapping := func(m map[string]string, from, to string) {
		dmf, err := parseDatasetMapFilter(m, false)
		if err != nil {
			t.Logf("expect test map to be valid: %s", err)
			t.FailNow()
		}
		fromPath, err := zfs.NewDatasetPath(from)
		if err != nil {
			t.Logf("expect test from path to be valid: %s", err)
			t.FailNow()
		}
		toPath, err := zfs.NewDatasetPath(to)
		if err != nil {
			t.Logf("expect test to path to be valid: %s", err)
			t.FailNow()
		}

		res, err := dmf.Map(fromPath)
		t.Logf("%s => %s", fromPath.ToString(), res.ToString())
		assert.Nil(t, err)
		assert.True(t, res.Equal(toPath))
	}

	expectFilter := func(m map[string]string, path string, pass bool) {
		dmf, err := parseDatasetMapFilter(m, true)
		if err != nil {
			t.Logf("expect test filter to be valid: %s", err)
			t.FailNow()
		}
		p, err := zfs.NewDatasetPath(path)
		if err != nil {
			t.Logf("expect test path to be valid: %s", err)
			t.FailNow()
		}
		res, err := dmf.Filter(p)
		assert.Nil(t, err)
		assert.Equal(t, pass, res)
	}

	map1 := map[string]string{
		"a/b/c<": "root1",
		"a/b<":   "root2",
		"<":      "root3/b/c",
		"q<":     "root4/1/2",
	}

	expectMapping(map1, "a/b/c", "root1")
	expectMapping(map1, "a/b/c/d", "root1/d")
	expectMapping(map1, "a/b/e", "root2/e")
	expectMapping(map1, "a/b", "root2")
	expectMapping(map1, "x", "root3/b/c")
	expectMapping(map1, "x/y", "root3/b/c/y")
	expectMapping(map1, "q", "root4/1/2")
	expectMapping(map1, "q/r", "root4/1/2/r")

	filter1 := map[string]string{
		"<":    "omit",
		"a<":   "ok",
		"a/b<": "omit",
	}

	expectFilter(filter1, "b", false)
	expectFilter(filter1, "a", true)
	expectFilter(filter1, "a/d", true)
	expectFilter(filter1, "a/b", false)
	expectFilter(filter1, "a/b/c", false)

	filter2 := map[string]string{}
	expectFilter(filter2, "foo", false) // default to omit

}
