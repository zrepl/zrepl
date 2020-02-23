package config

import (
	"bufio"
	"bytes"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/kr/pretty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// template must be a template/text template with a single '{{ . }}' as placeholder for val
//nolint[:deadcode,unused]
func testValidConfigTemplate(t *testing.T, tmpl string, val string) *Config {
	tmp, err := template.New("master").Parse(tmpl)
	if err != nil {
		panic(err)
	}
	var buf bytes.Buffer
	err = tmp.Execute(&buf, val)
	if err != nil {
		panic(err)
	}
	return testValidConfig(t, buf.String())
}

func testValidConfig(t *testing.T, input string) *Config {
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

func trimSpaceEachLineAndPad(s, pad string) string {
	var out strings.Builder
	scan := bufio.NewScanner(strings.NewReader(s))
	for scan.Scan() {
		fmt.Fprintf(&out, "%s%s\n", pad, bytes.TrimSpace(scan.Bytes()))
	}
	return out.String()
}

func TestTrimSpaceEachLineAndPad(t *testing.T) {
	foo := `
	foo
	bar baz 
	`
	assert.Equal(t, "  \n  foo\n  bar baz\n  \n", trimSpaceEachLineAndPad(foo, "  "))
}
