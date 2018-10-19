package config

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zrepl/yaml-config"
	"testing"
)

func testValidGlobalSection(t *testing.T, s string) *Config {
	jobdef := `
jobs:
- name: dummyjob
  type: sink
  serve:
    type: tcp
    listen: ":2342"
    clients: {
      "10.0.0.1":"foo"
    }
  root_fs: zoot/foo
`
	_, err := ParseConfigBytes([]byte(jobdef))
	require.NoError(t, err)
	return testValidConfig(t, s + jobdef)
}

func TestOutletTypes(t *testing.T) {
	conf := testValidGlobalSection(t, `
global:
  logging:
  - type: stdout
    level: debug
    format: human
  - type: syslog
    level: info
    retry_interval: 20s
    format: human
  - type: tcp
    level: debug
    format: json
    address: logserver.example.com:1234
  - type: tcp
    level: debug
    format: json
    address: encryptedlogserver.example.com:1234
    retry_interval: 20s 
    tls:
      ca: /etc/zrepl/log/ca.crt
      cert: /etc/zrepl/log/key.pem
      key: /etc/zrepl/log/cert.pem
`)
	assert.Equal(t, 4, len(*conf.Global.Logging))
	assert.NotNil(t, (*conf.Global.Logging)[3].Ret.(*TCPLoggingOutlet).TLS)
}

func TestDefaultLoggingOutlet(t *testing.T) {
	conf := testValidGlobalSection(t, "")
	assert.Equal(t, 1, len(*conf.Global.Logging))
	o := (*conf.Global.Logging)[0].Ret.(*StdoutLoggingOutlet)
	assert.Equal(t, "warn", o.Level)
	assert.Equal(t, "human", o.Format)
}

func TestPrometheusMonitoring(t *testing.T) {
	conf := testValidGlobalSection(t, `
global:
  monitoring:
    - type: prometheus
      listen: ':9091'
`)
	assert.Equal(t, ":9091", conf.Global.Monitoring[0].Ret.(*PrometheusMonitoring).Listen)	
}

func TestLoggingOutletEnumList_SetDefaults(t *testing.T) {
	e := &LoggingOutletEnumList{}
	var i yaml.Defaulter = e
	require.NotPanics(t, func() {
		i.SetDefault()
		assert.Equal(t, "warn", (*e)[0].Ret.(*StdoutLoggingOutlet).Level)
	})
}
