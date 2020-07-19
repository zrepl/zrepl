package config

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zrepl/yaml-config"
)

type A struct {
	B  *B     `yaml:"b,optional,fromdefaults"`
	A1 string `yaml:"a1,optional"`
}

type B struct {
	C *C     `yaml:"c,optional,fromdefaults"`
	D string `yaml:"d,default=ddd"`
	E string `yaml:"e,optional"`
}

type C struct {
	Q string `yaml:"q,optional"`
	R string `yaml:"r,default=r"`
}

func TestDepFromDefaults(t *testing.T) {

	type testcase struct {
		name   string
		yaml   string
		expect *A
	}

	tcs := []testcase{
		{
			name: "empty",
			yaml: `{}`,
			expect: &A{
				B: &B{
					C: &C{
						R: "r",
					},
					D: "ddd",
				},
			},
		},
		{
			name: "a1 set",
			yaml: `{"a1":"blah"}`,
			expect: &A{
				A1: "blah",
				B: &B{
					C: &C{
						R: "r",
					},
					D: "ddd",
				},
			},
		},
		{
			name: "D set",
			yaml: `
b:
  d: 4d
`,
			expect: &A{
				B: &B{
					D: "4d",
					C: &C{
						R: "r",
					},
				},
			},
		},
	}

	for tci := range tcs {
		t.Run(fmt.Sprintf("%d-%s", tci, tcs[tci].name), func(t *testing.T) {
			tc := tcs[tci]

			var a A
			err := yaml.UnmarshalStrict([]byte(tc.yaml), &a)
			require.NoError(t, err)

			require.Equal(t, tc.expect, &a)
		})
	}

}
