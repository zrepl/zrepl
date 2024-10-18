package tcp

import (
	"net"
	"os"
	"testing"

	"github.com/kr/pretty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIPMap(t *testing.T) {
	type testCaseExpect struct {
		expectNoMapping bool
		expectIdent     string
	}
	type testCase struct {
		name          string
		config        map[string]string
		expectInitErr bool
		expect        map[string]testCaseExpect
	}

	cases := []testCase{
		{
			name:          "regular ips",
			expectInitErr: false,
			config: map[string]string{
				"192.168.123.234": "ident1",
				"192.168.20.23":   "ident2",
			},
			expect: map[string]testCaseExpect{
				"192.168.123.234": {expectIdent: "ident1"},
				"192.168.20.23":   {expectIdent: "ident2"},
				"192.168.20.10":   {expectNoMapping: true},
			},
		},
		{
			name:          "full wildcard",
			expectInitErr: false,
			config: map[string]string{
				"0.0.0.0/0": "*",
				"0::/0":     "*",
			},
			expect: map[string]testCaseExpect{
				"10.123.234.24": {expectIdent: "10.123.234.24"},
				// '[' and ']' are forbiddenin dataset names
				"fe80::23:42": {expectIdent: "fe80::23:42"},
			},
		},
		{
			name:          "longest prefix matching",
			expectInitErr: false,
			config: map[string]string{
				"10.1.2.3":              "specific-host",
				"10.1.2.0/24":           "subnet-one-two-*",
				"10.1.1.0/24":           "subnet-one-one-*",
				"10.1.0.0/24":           "subnet-one-zero-*",
				"10.1.0.0/16":           "subnet-one-*",
				"fde4:8dba:82e1:1::1":   "v6-48-1-specialhost",
				"fde4:8dba:82e1::/48":   "v6-48-*",
				"fde4:8dba:82e1:1::/64": "v6-64-1-*",
				"fde4:8dba:82e1:2::/64": "v6-64-2-*",
			},
			expect: map[string]testCaseExpect{
				"10.1.2.3":             {expectIdent: "specific-host"},
				"10.1.2.1":             {expectIdent: "subnet-one-two-10.1.2.1"},
				"10.1.1.1":             {expectIdent: "subnet-one-one-10.1.1.1"},
				"10.1.0.23":            {expectIdent: "subnet-one-zero-10.1.0.23"},
				"10.1.3.1":             {expectIdent: "subnet-one-10.1.3.1"},
				"10.2.1.1":             {expectNoMapping: true},
				"fde4:8dba:82e1:1::1":  {expectIdent: "v6-48-1-specialhost"},
				"fde4:8dba:82e1:23::1": {expectIdent: "v6-48-fde4:8dba:82e1:23::1"},
				"fde4:8dba:82e1:1::2":  {expectIdent: "v6-64-1-fde4:8dba:82e1:1::2"},
				"fde4:8dba:82e1:2::1":  {expectIdent: "v6-64-2-fde4:8dba:82e1:2::1"},
				"fde4:8dba:82e2::1":    {expectNoMapping: true},
			},
		},
		{
			name:          "different prefixes, mixed ipv4 ipv6, with interface ids",
			expectInitErr: false,
			config: map[string]string{
				"192.168.23.0/24": "db-*",
				"192.168.23.23":   "db-twentythree",
				"192.168.42.0/24": "web-*",
				"10.1.4.0/24":     "my-*-server",
				"2001:0db8:85a3:0000:0000:8a2e:0370:7334":      "aspecifichost",
				"2001:0db8:85a3:0000:0000:8a2e:0370:7334%eth1": "aspecifichost",
				"fe80::/16%eth1":      "san-*",
				"fde4:8dba:82e1::/64": "sub64-*",
			},
			expect: map[string]testCaseExpect{
				"10.1.2.3":      {expectNoMapping: true},
				"192.168.23.1":  {expectIdent: "db-192.168.23.1"},
				"192.168.42.1":  {expectIdent: "web-192.168.42.1"},
				"192.168.23.23": {expectIdent: "db-twentythree"},
				"10.1.4.5":      {expectIdent: "my-10.1.4.5-server"},

				// v6 matching
				"fe80::23:42%eth1": {expectIdent: "san-fe80::23:42-eth1"},
				"fe80::23:42%eth2": {expectNoMapping: true},

				// v6 subnet matching
				"fde4:8dba:82e1::1": {expectIdent: "sub64-fde4:8dba:82e1::1"},
				// v6 subnet matching with suffix that matches another allowed IPv4
				"fde4:8dba:82e1::c0a8:1717": {expectIdent: "sub64-fde4:8dba:82e1::c0a8:1717"},

				"2001:0db8:85a3:0000:0000:8a2e:0370:7334":      {expectIdent: "aspecifichost"},
				"2001:0db8:85a3:0000:0000:8a2e:0370:7334%eth1": {expectIdent: "aspecifichost"},
				"2001:0db8:85a3:0000:0000:8a2e:0370:7334%eth2": {expectNoMapping: true},
			},
		},
		{
			name:          "invalid user input: non ip or cidr",
			expectInitErr: true,
			config: map[string]string{
				"jimmy": "db",
			},
		},
		{
			name:          "invalid user input: v4 ip with an identity containing *",
			expectInitErr: true,
			config: map[string]string{
				"192.168.1.2": "db-*",
			},
		},
		{
			name:          "invalid user input: v4 subnet without an identity containing *",
			expectInitErr: true,
			config: map[string]string{
				"192.168.1.0/24": "db-",
			},
		},
		{
			name:          "invalid user input: v6 ip with an identity containing *",
			expectInitErr: true,
			config: map[string]string{
				"2001:0db8:85a3:0000:0000:8a2e:0370:7334": "aspecifichost*",
			},
		},
		{
			name:          "invalid user input: v6 subnet without identity containing *",
			expectInitErr: true,
			config: map[string]string{
				"fe80::/16%eth1": "db-",
			},
		},
		{
			name:          "invalid user input with subnet match: client identity with forbidden zfs dataset name char @",
			expectInitErr: true,
			config: map[string]string{
				"fe80::/16": "db@-*",
			},
		},
		{
			name:          "invalid user input with IP match: client identity with forbidden zfs dataset name char @",
			expectInitErr: true,
			config: map[string]string{
				"fe80::1": "db@foo",
			},
		},
	}

	for i := range cases {
		c := cases[i]
		t.Run(c.name, func(t *testing.T) {
			pretty.Fprintf(os.Stderr, "running %#v\n", c)
			m, err := ipMapFromConfig(c.config)
			if c.expectInitErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, m)
			}
			for input, expect := range c.expect {
				// reuse newIPMapEntry to parse test case input
				// "test" is not used during testing but must not be empty.
				ipMapEntry, err := newIPMapEntry(input, "test")
				require.NoError(t, err)
				ones, bits := ipMapEntry.subnet.Mask.Size()
				require.Equal(t, bits, net.IPv6len*8, "and we know ipMapEntry always expands its IPs to 16bytes")
				require.Equal(t, ones, net.IPv6len*8, "test case addresses must be fully specified")
				require.NotNil(t, ipMapEntry)
				ident, err := m.Get(&net.IPAddr{
					IP:   ipMapEntry.subnet.IP,
					Zone: ipMapEntry.zone,
				})
				if expect.expectNoMapping {
					assert.Empty(t, ident)
				} else {
					assert.NoError(t, err)
					assert.Equal(t, expect.expectIdent, ident)
				}
			}
		})
	}
}

func TestPackageNetAssumptions(t *testing.T) {

}
