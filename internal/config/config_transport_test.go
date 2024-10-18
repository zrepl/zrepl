package config

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTransportConnect(t *testing.T) {
	tmpl := `
jobs:
- name: foo
  type: push
  connect:
%s
  filesystems: {"<": true}
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

	mconf := func(s string) string { return fmt.Sprintf(tmpl, s) }

	type test struct {
		Name        string
		ExpectError bool
		Connect     string
	}

	testTable := []test{
		{
			Name:        "tcp_with_address_and_port",
			ExpectError: false,
			Connect: `
			type: tcp
			address: 10.0.0.23:42
			`,
		},
		{
			Name:        "tls_with_host_and_port",
			ExpectError: false,
			Connect: `
			type: tls
			address: "server1.foo.bar:8888"
			ca:   /etc/zrepl/ca.crt
			cert: /etc/zrepl/backupserver.fullchain
			key:  /etc/zrepl/backupserver.key
			server_cn: "server1"
			`,
		},
		{
			Name:        "tcp_without_port",
			ExpectError: true,
			Connect: `
			type: tcp
			address: 10.0.0.23
			`,
		},
		{
			Name:        "tls_without_port",
			ExpectError: true,
			Connect: `
			type: tls
			address: 10.0.0.23
			ca:   /etc/zrepl/ca.crt
			cert: /etc/zrepl/backupserver.fullchain
			key:  /etc/zrepl/backupserver.key
			server_cn: "server1"
			`,
		},
	}

	for _, tc := range testTable {
		t.Run(tc.Name, func(t *testing.T) {
			require.NotEmpty(t, tc.Connect)
			connect := trimSpaceEachLineAndPad(tc.Connect, "    ")
			conf := mconf(connect)
			config, err := testConfig(t, conf)
			if tc.ExpectError && err == nil {
				t.Errorf("expected test failure, but got valid config %v", config)
				return
			}
			if !tc.ExpectError && err != nil {
				t.Errorf("not expecint test failure but got error: %s", err)
				return
			}
			t.Logf("error=%v", err)
			t.Logf("config=%v", config)
		})
	}

}
