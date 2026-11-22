package snapname

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWithoutPrefix(t *testing.T) {
	ts := time.Date(2026, 11, 22, 15, 4, 5, 0, time.UTC)
	tcs := map[string]string{
		"dense":           "20261122_150405_000",
		"human":           "2026-11-22_15:04:05",
		"iso-8601":        "2026-11-22T15:04:05.000Z",
		"unix-seconds":    "1795359845",
		"2006-01-02_1504": "2026-11-22_1504",
	}
	for format, expect := range tcs {
		t.Run(format, func(t *testing.T) {
			f, err := New("", format, "UTC")
			require.NoError(t, err)
			assert.Equal(t, "", f.Prefix())
			assert.Equal(t, expect, f.Format(ts))
		})
	}
}
