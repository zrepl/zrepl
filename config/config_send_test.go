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

	raw_true := `
  send:
    raw: true

`

	raw_false := `
  send:
    raw: false

`

	raw_and_encrypted := `
  send:
    encrypted: true
    raw: true
`

	properties_and_encrypted := `
  send:
    encrypted: true
    send_properties: true
`

	properties_true := `
  send:
    send_properties: true
`

	properties_false := `
  send:
    send_properties: false
`

	send_empty := `
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

	t.Run("send_empty", func(t *testing.T) {
		c := testValidConfig(t, fill(send_empty))
		encrypted := c.Jobs[0].Ret.(*PushJob).Send.Encrypted
		assert.Equal(t, false, encrypted)
	})

	t.Run("properties_and_encrypted", func(t *testing.T) {
		c := testValidConfig(t, fill(properties_and_encrypted))
		encrypted := c.Jobs[0].Ret.(*PushJob).Send.Encrypted
		properties := c.Jobs[0].Ret.(*PushJob).Send.SendProperties
		assert.Equal(t, true, encrypted)
		assert.Equal(t, true, properties)
	})

	t.Run("properties_false", func(t *testing.T) {
		c := testValidConfig(t, fill(properties_false))
		properties := c.Jobs[0].Ret.(*PushJob).Send.SendProperties
		assert.Equal(t, false, properties)
	})

	t.Run("properties_true", func(t *testing.T) {
		c := testValidConfig(t, fill(properties_true))
		properties := c.Jobs[0].Ret.(*PushJob).Send.SendProperties
		assert.Equal(t, true, properties)
	})

	t.Run("raw_true", func(t *testing.T) {
		c := testValidConfig(t, fill(raw_true))
		raw := c.Jobs[0].Ret.(*PushJob).Send.Raw
		assert.Equal(t, true, raw)
	})

	t.Run("raw_false", func(t *testing.T) {
		c := testValidConfig(t, fill(raw_false))
		raw := c.Jobs[0].Ret.(*PushJob).Send.Raw
		assert.Equal(t, false, raw)
	})

	t.Run("raw_and_encrypted", func(t *testing.T) {
		c := testValidConfig(t, fill(raw_and_encrypted))
		raw := c.Jobs[0].Ret.(*PushJob).Send.Raw
		encrypted := c.Jobs[0].Ret.(*PushJob).Send.Encrypted
		assert.Equal(t, true, raw)
		assert.Equal(t, true, encrypted)
	})

	t.Run("send_not_specified", func(t *testing.T) {
		c, err := testConfig(t, fill(send_not_specified))
		assert.NoError(t, err)
		assert.NotNil(t, c)
	})

}
