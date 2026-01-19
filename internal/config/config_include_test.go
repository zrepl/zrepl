package config

import (
	"testing"

	"github.com/kr/pretty"
	"github.com/stretchr/testify/require"
)

func TestIncludeSingle(t *testing.T) {
	path := "./samples/include.yml"

	t.Run(path, func(t *testing.T) {
		config, err := ParseConfig(path)
		if err != nil {
			t.Errorf("error parsing %s:\n%+v", path, err)
		}

		require.NotNil(t, config)
		require.NotNil(t, config.Global)
		require.NotEmpty(t, config.Jobs)

		t.Logf("file: %s", path)
		t.Log(pretty.Sprint(config))
	})
}

func TestIncludeDirectory(t *testing.T) {
	path := "./samples/include_directory.yml"

	t.Run(path, func(t *testing.T) {
		config, err := ParseConfig(path)
		if err != nil {
			t.Errorf("error parsing %s:\n%+v", path, err)
		}

		require.NotNil(t, config)
		require.NotNil(t, config.Global)
		require.NotEmpty(t, config.Jobs)

		t.Logf("file: %s", path)
		t.Log(pretty.Sprint(config))
	})
}
