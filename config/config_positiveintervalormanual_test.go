package config

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/zrepl/yaml-config"
)

func TestPositiveDurationOrManual(t *testing.T) {
	cases := []struct {
		Comment, Input string
		Result         *PositiveDurationOrManual
	}{
		{"empty is error", "", nil},
		{"negative is error", "-1s", nil},
		{"zero seconds is error", "0s", nil},
		{"zero is error", "0", nil},
		{"non-manual is error", "something", nil},
		{"positive seconds works", "1s", &PositiveDurationOrManual{Manual: false, Interval: 1 * time.Second}},
		{"manual works", "manual", &PositiveDurationOrManual{Manual: true, Interval: 0}},
	}
	for _, tc := range cases {
		t.Run(tc.Comment, func(t *testing.T) {
			var out struct {
				FieldName PositiveDurationOrManual `yaml:"fieldname"`
			}
			input := fmt.Sprintf("\nfieldname: %s\n", tc.Input)
			err := yaml.UnmarshalStrict([]byte(input), &out)
			if tc.Result == nil {
				assert.Error(t, err)
				t.Logf("%#v", out)
			} else {
				assert.Equal(t, *tc.Result, out.FieldName)
			}
		})
	}

}
