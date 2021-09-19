package bandwidthlimit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNoLimitConfig(t *testing.T) {

	conf := NoLimitConfig()

	err := ValidateConfig(conf)
	require.NoError(t, err)

	require.NotPanics(t, func() {
		_ = WrapperFromConfig(conf)
	})
}
