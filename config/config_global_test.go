package config

import (
	"fmt"
	"log/syslog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zrepl/yaml-config"
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
  root_fs: zroot/foo
`
	_, err := ParseConfigBytes([]byte(jobdef))
	require.NoError(t, err)
	return testValidConfig(t, s+jobdef)
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

func TestSyslogLoggingOutletFacility(t *testing.T) {
	type SyslogFacilityPriority struct {
		Facility string
		Priority syslog.Priority
	}
	syslogFacilitiesPriorities := []SyslogFacilityPriority{
		{"", syslog.LOG_LOCAL0}, // default
		{"kern", syslog.LOG_KERN}, {"daemon", syslog.LOG_DAEMON}, {"auth", syslog.LOG_AUTH},
		{"syslog", syslog.LOG_SYSLOG}, {"lpr", syslog.LOG_LPR}, {"news", syslog.LOG_NEWS},
		{"uucp", syslog.LOG_UUCP}, {"cron", syslog.LOG_CRON}, {"authpriv", syslog.LOG_AUTHPRIV},
		{"ftp", syslog.LOG_FTP}, {"local0", syslog.LOG_LOCAL0}, {"local1", syslog.LOG_LOCAL1},
		{"local2", syslog.LOG_LOCAL2}, {"local3", syslog.LOG_LOCAL3}, {"local4", syslog.LOG_LOCAL4},
		{"local5", syslog.LOG_LOCAL5}, {"local6", syslog.LOG_LOCAL6}, {"local7", syslog.LOG_LOCAL7},
	}

	for _, sFP := range syslogFacilitiesPriorities {
		logcfg := fmt.Sprintf(`
global:
  logging:
  - type: syslog
    level: info
    format: human
    facility: %s
`, sFP.Facility)
		conf := testValidGlobalSection(t, logcfg)
		assert.Equal(t, 1, len(*conf.Global.Logging))
		assert.True(t, SyslogFacility(sFP.Priority) == *(*conf.Global.Logging)[0].Ret.(*SyslogLoggingOutlet).Facility)
	}
}

func TestLoggingOutletEnumList_SetDefaults(t *testing.T) {
	e := &LoggingOutletEnumList{}
	var i yaml.Defaulter = e
	require.NotPanics(t, func() {
		i.SetDefault()
		assert.Equal(t, "warn", (*e)[0].Ret.(*StdoutLoggingOutlet).Level)
	})
}
