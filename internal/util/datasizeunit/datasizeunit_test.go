package datasizeunit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zrepl/yaml-config"
)

func TestBits(t *testing.T) {

	tcs := []struct {
		input      string
		expectRate float64
		expectErr  string
	}{
		{`23 bit`, 23, ""}, // bit special case works
		{`23bit`, 23, ""},  // also without space

		{`10MiB`, 10 * (1 << 20) * 8, ""},  // integer unit without space
		{`10 MiB`, 8 * 10 * (1 << 20), ""}, // integer unit with space

		{`10.5 Kib`, 10.5 * (1 << 10), ""}, // floating point with bit unit works with space
		{`10.5Kib`, 10.5 * (1 << 10), ""},  // floating point with bit unit works without space

		// unit checks
		{`1 bit`, 1, ""},
		{`1 B`, 1 * 8, ""},
		{`1 Kb`, 1e3, ""},
		{`1 Kib`, 1 << 10, ""},
		{`1 Mb`, 1e6, ""},
		{`1 Mib`, 1 << 20, ""},
		{`1 Gb`, 1e9, ""},
		{`1 Gib`, 1 << 30, ""},
		{`1 Tb`, 1e12, ""},
		{`1 Tib`, 1 << 40, ""},
	}

	for _, tc := range tcs {
		t.Run(tc.input, func(t *testing.T) {

			var bits Bits
			err := yaml.Unmarshal([]byte(tc.input), &bits)
			if tc.expectErr != "" {
				assert.Error(t, err)
				assert.Regexp(t, tc.expectErr, err.Error())
				assert.Zero(t, bits.bits)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectRate, bits.bits)
			}
		})

	}

}
