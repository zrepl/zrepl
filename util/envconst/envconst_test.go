package envconst_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/util/envconst"
)

type ExampleVarType struct{ string }

var (
	Var1 = ExampleVarType{"var1"}
	Var2 = ExampleVarType{"var2"}
)

func (m ExampleVarType) String() string { return string(m.string) }
func (m *ExampleVarType) Set(s string) error {
	switch s {
	case Var1.String():
		*m = Var1
	case Var2.String():
		*m = Var2
	default:
		return fmt.Errorf("unknown var %q", s)
	}
	return nil
}

const EnvVarName = "ZREPL_ENVCONST_UNIT_TEST_VAR"

func TestVar(t *testing.T) {
	_, set := os.LookupEnv(EnvVarName)
	require.False(t, set)
	defer os.Unsetenv(EnvVarName)

	val := envconst.Var(EnvVarName, &Var1)
	if &Var1 != val {
		t.Errorf("default value shut be same address")
	}

	err := os.Setenv(EnvVarName, "var2")
	require.NoError(t, err)

	val = envconst.Var(EnvVarName, &Var1)
	require.Equal(t, &Var2, val, "only structural identity is required for non-default vars")
}
