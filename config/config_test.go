package config

import (
	"github.com/kr/pretty"
	"path"
	"path/filepath"
	"testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zrepl/yaml-config"
)

func TestSampleConfigsAreParsedWithoutErrors(t *testing.T) {
	paths, err := filepath.Glob("./samples/*")
	if err != nil {
		t.Errorf("glob failed: %+v", err)
	}

	for _, p := range paths {

		if path.Ext(p) != ".yml" {
			t.Logf("skipping file %s", p)
			continue
		}

		t.Run(p, func(t *testing.T) {
			c, err := ParseConfig(p)
			if err != nil {
				t.Errorf("error parsing %s:\n%+v", p, err)
			}

			t.Logf("file: %s", p)
			t.Log(pretty.Sprint(c))
		})

	}

}

func TestLoggingOutletEnumList_SetDefaults(t *testing.T) {
	e := &LoggingOutletEnumList{}
	var i yaml.Defaulter = e
	require.NotPanics(t, func() {
		i.SetDefault()
		assert.Equal(t, "warn", (*e)[0].Ret.(StdoutLoggingOutlet).Level)
	})
}


func testValidConfig(t *testing.T, input string) (*Config) {
	t.Helper()
	conf, err := testConfig(t, input)
	require.NoError(t, err)
	require.NotNil(t, conf)
	return conf
}

func testConfig(t *testing.T, input string) (*Config, error) {
	t.Helper()
	return ParseConfigBytes([]byte(input))
}