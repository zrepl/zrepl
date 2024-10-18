package snapper

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zrepl/yaml-config"

	"github.com/zrepl/zrepl/internal/config"
)

func TestCronLibraryWorks(t *testing.T) {

	type testCase struct {
		spec   string
		in     time.Time
		expect time.Time
	}
	dhm := func(day, hour, minutes int) time.Time {
		return time.Date(2022, 7, day, hour, minutes, 0, 0, time.UTC)
	}
	hm := func(hour, minutes int) time.Time {
		return dhm(23, hour, minutes)
	}

	tcs := []testCase{
		{"0-10 * * * *", dhm(17, 1, 10), dhm(17, 2, 0)},
		{"0-10 * * * *", dhm(17, 23, 10), dhm(18, 0, 0)},
		{"0-10 * * * *", hm(1, 9), hm(1, 10)},
		{"0-10 * * * *", hm(1, 9), hm(1, 10)},

		{"1,3,5 * * * *", hm(1, 1), hm(1, 3)},
		{"1,3,5 * * * *", hm(1, 2), hm(1, 3)},
		{"1,3,5 * * * *", hm(1, 3), hm(1, 5)},
		{"1,3,5 * * * *", hm(1, 5), hm(2, 1)},

		{"* 0-5,8,12 * * *", hm(0, 0), hm(0, 1)},
		{"* 0-5,8,12 * * *", hm(4, 59), hm(5, 0)},
		{"* 0-5,8,12 * * *", hm(5, 0), hm(5, 1)},
		{"* 0-5,8,12 * * *", hm(5, 59), hm(8, 0)},
		{"* 0-5,8,12 * * *", hm(8, 59), hm(12, 0)},

		// https://github.com/zrepl/zrepl/pull/614#issuecomment-1188358989
		{"53 17,18,19 * * *", dhm(23, 17, 52), dhm(23, 17, 53)},
		{"53 17,18,19 * * *", dhm(23, 17, 53), dhm(23, 18, 53)},
		{"53 17,18,19 * * *", dhm(23, 18, 53), dhm(23, 19, 53)},
		{"53 17,18,19 * * *", dhm(23, 19, 53), dhm(24 /* ! */, 17, 53)},
	}

	for i, tc := range tcs {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			var s struct {
				Cron config.CronSpec `yaml:"cron"`
			}
			inp := fmt.Sprintf("cron: %q", tc.spec)
			fmt.Println("spec is ", inp)
			err := yaml.UnmarshalStrict([]byte(inp), &s)
			require.NoError(t, err)

			actual := s.Cron.Schedule.Next(tc.in)
			assert.Equal(t, tc.expect, actual)
		})
	}

}
