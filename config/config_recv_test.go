package config

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	zfsprop "github.com/zrepl/zrepl/zfs/property"
)

func TestRecvOptions(t *testing.T) {
	tmpl := `
jobs:
- name: foo
  type: pull
  connect:
    type: local
    listener_name: foo
    client_identity: bar
  root_fs: "zreplplatformtest"
  %s
  interval: manual
  pruning:
    keep_sender:
    - type: last_n
      count: 10
    keep_receiver:
    - type: last_n
      count: 10

`

	recv_properties_empty := `
  recv:
    properties:
`

	recv_inherit_empty := `
  recv:
    properties:
      inherit:
`

	recv_inherit := `
  recv:
    properties:
      inherit:
        - testprop
`

	recv_override_empty := `
  recv:
    properties:
      override:
`

	recv_override := `
  recv:
    properties:
      override:
        testprop2: "test123"
`

	recv_override_and_inherit := `
  recv:
    properties:
      inherit:
        - testprop
      override:
        testprop2: "test123"
`

	recv_empty := `
  recv: {}
`

	recv_not_specified := `
`

	fill := func(s string) string { return fmt.Sprintf(tmpl, s) }

	t.Run("recv_inherit_empty", func(t *testing.T) {
		c := testValidConfig(t, fill(recv_inherit_empty))
		assert.NotNil(t, c)
	})

	t.Run("recv_inherit", func(t *testing.T) {
		c := testValidConfig(t, fill(recv_inherit))
		inherit := c.Jobs[0].Ret.(*PullJob).Recv.Properties.Inherit
		assert.NotEmpty(t, inherit)
		assert.Contains(t, inherit, zfsprop.Property("testprop"))
	})

	t.Run("recv_override_empty", func(t *testing.T) {
		c := testValidConfig(t, fill(recv_override_empty))
		assert.NotNil(t, c)
	})

	t.Run("recv_override", func(t *testing.T) {
		c := testValidConfig(t, fill(recv_override))
		override := c.Jobs[0].Ret.(*PullJob).Recv.Properties.Override
		require.Len(t, override, 1)
		require.Equal(t, "test123", override["testprop2"])

	})

	t.Run("recv_override_and_inherit", func(t *testing.T) {
		c := testValidConfig(t, fill(recv_override_and_inherit))
		inherit := c.Jobs[0].Ret.(*PullJob).Recv.Properties.Inherit
		override := c.Jobs[0].Ret.(*PullJob).Recv.Properties.Override
		assert.NotEmpty(t, inherit)
		assert.Contains(t, inherit, zfsprop.Property("testprop"))
		assert.NotEmpty(t, override)
		assert.Equal(t, "test123", override["testprop2"])
	})

	t.Run("recv_properties_empty", func(t *testing.T) {
		c := testValidConfig(t, fill(recv_properties_empty))
		assert.NotNil(t, c)
	})

	t.Run("recv_empty", func(t *testing.T) {
		c := testValidConfig(t, fill(recv_empty))
		assert.NotNil(t, c)
	})

	t.Run("send_not_specified", func(t *testing.T) {
		c := testValidConfig(t, fill(recv_not_specified))
		assert.NotNil(t, c)
	})

}
