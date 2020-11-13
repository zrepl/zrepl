package tcp

import (
	"bytes"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"

	"github.com/zrepl/zrepl/transport"
)

type ipMapEntry struct {
	subnet *net.IPNet
	// ident is always not empty
	ident string
	// zone may be empty (e.g. for IPv4)
	zone string
}

type ipMap struct {
	entries []*ipMapEntry
}

func ipMapFromConfig(clients map[string]string) (*ipMap, error) {

	entries := make([]*ipMapEntry, 0, len(clients))
	for clientInput, clientIdent := range clients {
		userIPMapEntry, err := newIPMapEntry(clientInput, clientIdent)
		if err != nil {
			return nil, errors.Wrapf(err, "cannot not parse %q:%q", clientInput, clientIdent)
		}

		entries = append(entries, userIPMapEntry)
	}

	sort.Sort(byPrefixlen(entries))

	return &ipMap{entries: entries}, nil
}

func (m *ipMap) Get(ipAddr *net.IPAddr) (string, error) {
	for _, e := range m.entries {
		if e.zone != ipAddr.Zone {
			continue
		}
		if e.subnet.Contains(ipAddr.IP) {
			return zfsDatasetPathComponentCompatibleRepresentation(e.ident, ipAddr), nil
		}
	}
	return "", errors.Errorf("no identity mapping for client IP: %s%%%s", ipAddr.IP, ipAddr.Zone)
}

var ipv6FullySpecifiedMask = bytes.Repeat([]byte{0xff}, net.IPv6len)

func newIPMapEntry(input string, ident string) (*ipMapEntry, error) {
	ip := input
	var zone string

	ipZoneSplit := strings.SplitN(input, "%", 2)
	if len(ipZoneSplit) > 1 {
		ip = ipZoneSplit[0]
		zone = ipZoneSplit[1]
	}

	_, subnet, err := net.ParseCIDR(ip)
	if err != nil {
		// expect full IP, no '*' placeholder expansion

		if strings.Count(ident, "*") != 0 {
			return nil, fmt.Errorf("non-CIDR matches must not contain '*' placeholder")
		}

		if err := transport.ValidateClientIdentity(ident); err != nil {
			return nil, errors.Wrapf(err, "invalid client identity %q for IP %q", ident, ip)
		}

		parsedIP := net.ParseIP(ip)
		if parsedIP == nil {
			return nil, errors.Wrapf(err, "invalid client address %q", ip)
		}
		parsedIP = parsedIP.To16()

		return &ipMapEntry{
			subnet: &net.IPNet{
				IP:   parsedIP,
				Mask: ipv6FullySpecifiedMask,
			},
			zone:  zone,
			ident: ident,
		}, nil
	}

	// expect CIDR and '*' placeholder expansion

	if strings.Count(ident, "*") != 1 {
		err = fmt.Errorf("CIDRs require 1 IP placeholder")
		return nil, errors.Wrapf(err, "invalid client identity %q for IP %q", ident, ip)
	}

	longestIPAddr := net.IPAddr{
		IP:   net.IP(bytes.Repeat([]byte{0xff}, net.IPv6len)),
		Zone: strings.Repeat("i", unix.IFNAMSIZ),
	}
	longestIdent := zfsDatasetPathComponentCompatibleRepresentation(ident, &longestIPAddr)

	if err := transport.ValidateClientIdentity(longestIdent); err != nil {
		return nil, errors.Wrapf(err, "invalid client identity for IP %q", ip)
	}

	ones, _ := subnet.Mask.Size()
	preExpansionAddrlen := len(subnet.IP) * 8
	expanded := subnet.IP.To16()
	postExpansionAddrlen := len(expanded) * 8
	return &ipMapEntry{
		subnet: &net.IPNet{
			IP:   expanded,
			Mask: net.CIDRMask(postExpansionAddrlen-preExpansionAddrlen+ones, postExpansionAddrlen),
		},
		zone:  zone,
		ident: ident,
	}, nil
}

func zfsDatasetPathComponentCompatibleRepresentation(identityWithWildcard string, addr *net.IPAddr) string {
	// If a Zone exists we append it after the IP using a "-" because "%"
	// is a zfs dataset forbidden char.
	if addr.Zone != "" {
		return strings.Replace(identityWithWildcard, "*", addr.IP.String()+"-"+addr.Zone, 1)
	}
	// newIPMapEntry validates that the line contains exactly one "*"
	return strings.Replace(identityWithWildcard, "*", addr.IP.String(), 1)
}

func (e *ipMapEntry) String() string {
	return fmt.Sprintf("&ipMapEntry{subnet=%q, ident=%q, zone=%q}", e.subnet.String(), e.ident, e.zone)
}

func (e *ipMapEntry) PrefixLen() int {
	ones, bits := e.subnet.Mask.Size()
	if bits != net.IPv6len*8 {
		panic(fmt.Sprintf("impl error: we represent all addresses as 16byte internally ones=%v bits=%v: %s", ones, bits, e))
	}
	return ones
}

// newtype to support sorting by prefixlength
type byPrefixlen []*ipMapEntry

func (m byPrefixlen) Len() int { return len(m) }
func (m byPrefixlen) Less(i, j int) bool {

	if m[i].PrefixLen() != m[j].PrefixLen() {
		return m[i].PrefixLen() > m[j].PrefixLen()
	}

	addrCmp := bytes.Compare(m[i].subnet.IP.To16(), m[j].subnet.IP.To16())
	if addrCmp != 0 {
		return addrCmp < 0
	}

	return strings.Compare(m[i].zone, m[j].zone) < 0
}
func (m byPrefixlen) Swap(i, j int) { m[i], m[j] = m[j], m[i] }
