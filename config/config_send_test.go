package config

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSendOptions(t *testing.T) {
	tmpl := `
jobs:
- name: foo
  type: push
  connect:
    type: local
    listener_name: foo
    client_identity: bar
  filesystems: {"<": true}
  %s
  snapshotting:
    type: manual
  pruning:
    keep_sender:
    - type: last_n
      count: 10
    keep_receiver:
    - type: last_n
      count: 10
`
	encrypted_false := `
  send:
    encrypted: false
`

	encrypted_true := `
  send:
    encrypted: true
`

	encrypted_unspecified := `
  send: {}
`

	send_not_specified := `
`

	fill := func(s string) string { return fmt.Sprintf(tmpl, s) }
	var c *Config

	t.Run("encrypted_false", func(t *testing.T) {
		c = testValidConfig(t, fill(encrypted_false))
		encrypted := c.Jobs[0].Ret.(*PushJob).Send.Encrypted
		assert.Equal(t, false, encrypted)
	})
	t.Run("encrypted_true", func(t *testing.T) {
		c = testValidConfig(t, fill(encrypted_true))
		encrypted := c.Jobs[0].Ret.(*PushJob).Send.Encrypted
		assert.Equal(t, true, encrypted)
	})

	t.Run("encrypted_unspecified", func(t *testing.T) {
		c, err := testConfig(t, fill(encrypted_unspecified))
		assert.Error(t, err)
		assert.Nil(t, c)
	})

	t.Run("send_not_specified", func(t *testing.T) {
		c, err := testConfig(t, fill(send_not_specified))
		assert.NoError(t, err)
		assert.NotNil(t, c)
	})

}
