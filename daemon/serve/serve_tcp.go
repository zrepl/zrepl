package serve

import (
	"github.com/zrepl/zrepl/config"
	"net"
	"github.com/pkg/errors"
	"context"
)

type TCPListenerFactory struct {
	address *net.TCPAddr
	clientMap *ipMap
}

type ipMapEntry struct {
	ip net.IP
	ident string
}

type ipMap struct {
	entries []ipMapEntry
}

func ipMapFromConfig(clients map[string]string) (*ipMap, error) {
	entries := make([]ipMapEntry, 0, len(clients))
	for clientIPString, clientIdent := range clients {
		clientIP := net.ParseIP(clientIPString)
		if clientIP == nil {
			return nil, errors.Errorf("cannot parse client IP %q", clientIPString)
		}
		if err := ValidateClientIdentity(clientIdent); err != nil {
			return nil, errors.Wrapf(err,"invalid client identity for IP %q", clientIPString)
		}
		entries = append(entries, ipMapEntry{clientIP, clientIdent})
	}
	return &ipMap{entries: entries}, nil
}

func (m *ipMap) Get(ip net.IP) (string, error) {
	for _, e := range m.entries {
		if e.ip.Equal(ip) {
			return e.ident, nil
		}
	}
	return "", errors.Errorf("no identity mapping for client IP %s", ip)
}

func TCPListenerFactoryFromConfig(c *config.Global, in *config.TCPServe) (*TCPListenerFactory, error) {
	addr, err := net.ResolveTCPAddr("tcp", in.Listen)
	if err != nil {
		return nil, errors.Wrap(err, "cannot parse listen address")
	}
	clientMap, err := ipMapFromConfig(in.Clients)
	if err != nil {
		return nil, errors.Wrap(err, "cannot parse client IP map")
	}
	lf := &TCPListenerFactory{
		address: addr,
		clientMap: clientMap,
	}
	return lf, nil
}

func (f *TCPListenerFactory) Listen() (AuthenticatedListener, error) {
	l, err := net.ListenTCP("tcp", f.address)
	if err != nil {
		return nil, err
	}
	return &TCPAuthListener{l, f.clientMap}, nil
}

type TCPAuthListener struct {
	*net.TCPListener
	clientMap *ipMap
}

func (f *TCPAuthListener) Accept(ctx context.Context) (AuthenticatedConn, error) {
	nc, err := f.TCPListener.Accept()
	if err != nil {
		return nil, err
	}
	clientIP := nc.RemoteAddr().(*net.TCPAddr).IP
	clientIdent, err := f.clientMap.Get(clientIP)
	if err != nil {
		getLogger(ctx).WithField("ip", clientIP).Error("client IP not in client map")
		nc.Close()
		return nil, err
	}
	return authConn{nc, clientIdent}, nil
}

