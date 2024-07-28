package timestamp_formatting

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var utc, _ = time.LoadLocation("UTC")
var berlin, _ = time.LoadLocation("Europe/Berlin")
var nyc, _ = time.LoadLocation("America/New_York")

func TestAssumptionsAboutTimePackage(t *testing.T) {
	now := time.Now()
	assert.Equal(t, now.In(utc).Unix(), now.In(berlin).Unix(), "unix timestamp is always in UTC")
}

func TestLegacyIso8601Format(t *testing.T) {
	// Before we allowed users to specify the location of the time to be formatted,
	// we always used UTC.
	// At the time, the `iso-8601` format used the following format string
	// "2006-01-02T15:04:05.000Z"
	// That format string's `Z` was never identified by the Go time package as a time zone specifier.
	// The correct way would have been
	// "2006-01-02T15:04:05.000Z07"
	// "2006-01-02T15:04:05.000Z0700"
	// "2006-01-02T15:04:05.000Z070000"
	// or any variation of how the minute / second offsets are displayed.
	// However, because of the forced location UTC, it didn't matter, because both format strings
	// evaluate to the same time string.
	//
	// This test is here to ensure that the legacy behavior is preserved for users who don't specify a non-UTC location
	// (UTC location is the default location at this time)

	oldFormatString := "2006-01-02T15:04:05.000Z"
	now := time.Now()
	oldOutput := now.In(utc).Format(oldFormatString)

	currentImpl, err := New("iso-8601", "UTC")
	require.NoError(t, err)
	currentOutput := currentImpl.Format(now)

	assert.Equal(t, oldOutput, currentOutput, "legacy behavior of iso-8601 format is preserved")
}

func TestIso8601PrintsTimeZoneOffset(t *testing.T) {
	f, err := New("iso-8601", "Europe/Berlin")
	require.NoError(t, err)
	out := f.Format(time.Date(2024, 5, 14, 21, 16, 23, 0, time.UTC))
	require.Equal(t, "2024-05-14T23:16:23.000_0200", out, "time zone offset is printed")
}

func TestBuiltinFormatsErrorOnNonUtcLocation(t *testing.T) {
	var err error

	expectMsg := `^.*: format string requires UTC location$`

	_, err = New("dense", "Europe/Berlin")
	require.Regexp(t, expectMsg, err)

	_, err = New("human", "Europe/Berlin")
	require.Regexp(t, expectMsg, err)

	_, err = New("unix-seconds", "Europe/Berlin")
	require.Regexp(t, expectMsg, err)

	_, err = New("iso-8601", "Europe/Berlin")
	require.NoError(t, err, "iso-8601 prints time zone, so non-UTC locations are allowed, see test TestIso8601PrintsTimeZoneOffset")
}

func TestPositiveUtcOffsetDetection(t *testing.T) {
	assert.True(t, isLocationPositiveOffsetToUTC(berlin), "Berlin is UTC+1/UTC+2")
	assert.False(t, isLocationPositiveOffsetToUTC(utc), "UTC is UTC+0")
	assert.False(t, isLocationPositiveOffsetToUTC(nyc), "New York is UTC-5/UTC-4")
}

func TestFormatCanReplacePlusWithUnderscore(t *testing.T) {
	f, err := New("2006-01-02_15:04:05-07:00:00", "Europe/Berlin")
	require.NoError(t, err)
	out := f.Format(time.Date(2024, 5, 14, 21, 16, 23, 0, time.UTC))
	require.Equal(t, "2024-05-14_23:16:23_02:00:00", out, "+ is replaced with _ so we can use the string in ZFS snapshots")
}
