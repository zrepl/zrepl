package config

import (
	"github.com/kr/pretty"
	"path/filepath"
	"testing"
)

func TestSampleConfigsAreParsedWithoutErrors(t *testing.T) {
	paths, err := filepath.Glob("./samples/*")
	if err != nil {
		t.Errorf("glob failed: %+v", err)
	}

	for _, p := range paths {

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
