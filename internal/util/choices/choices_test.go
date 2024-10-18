package choices_test

import (
	"bytes"
	"flag"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/internal/util/choices"
)

func TestChoices(t *testing.T) {

	var c choices.Choices

	fs := flag.NewFlagSet("testset", flag.ContinueOnError)
	c.Init("append", os.O_APPEND, "overwrite", os.O_TRUNC|os.O_CREATE)
	fs.Var(&c, "mode", c.Usage())
	var o bytes.Buffer
	fs.SetOutput(&o)

	fs.Usage()
	usage := o.String()
	o.Reset()

	t.Logf("usage:\n%s", usage)
	require.Contains(t, usage, "\"append\"")
	require.Contains(t, usage, "\"overwrite\"")

	err := fs.Parse([]string{"-mode", "append"})
	require.NoError(t, err)
	o.Reset()
	require.Equal(t, os.O_APPEND, c.Value())

	c.SetDefaultValue(nil)
	err = fs.Parse([]string{})
	require.NoError(t, err)
	o.Reset()
	require.Nil(t, c.Value())

	// a little whitebox testing: this is allowed ATM, we don't check that the default value was specified as a choice in init
	c.SetDefaultValue(os.O_RDWR)
	err = fs.Parse([]string{})
	require.NoError(t, err)
	o.Reset()
	require.Equal(t, os.O_RDWR, c.Value())

}
