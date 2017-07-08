package cmd

import (
	"github.com/stretchr/testify/assert"
	"github.com/zrepl/zrepl/util"
	"testing"
	"time"
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
