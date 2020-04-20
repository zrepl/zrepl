package tcp

import (
	"context"
	"net"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/transport"
	"github.com/zrepl/zrepl/util/tcpsock"
)

type ipMapEntry struct {
	ip    net.IP
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
		if err := transport.ValidateClientIdentity(clientIdent); err != nil {
			return nil, errors.Wrapf(err, "invalid client identity for IP %q", clientIPString)
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

func TCPListenerFactoryFromConfig(c *config.Global, in *config.TCPServe) (transport.AuthenticatedListenerFactory, error) {
	clientMap, err := ipMapFromConfig(in.Clients)
	if err != nil {
		return nil, errors.Wrap(err, "cannot parse client IP map")
	}
	lf := func() (transport.AuthenticatedListener, error) {
		l, err := tcpsock.Listen(in.Listen, in.ListenFreeBind)
		if err != nil {
			return nil, err
		}
		return &TCPAuthListener{l, clientMap}, nil
	}
	return lf, nil
}

type TCPAuthListener struct {
	*net.TCPListener
	clientMap *ipMap
}

func (f *TCPAuthListener) Accept(ctx context.Context) (*transport.AuthConn, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-ctx.Done()
		cancel()
	}()
	nc, err := f.TCPListener.AcceptTCP()
	if err != nil {
		return nil, err
	}
	clientIP := nc.RemoteAddr().(*net.TCPAddr).IP
	clientIdent, err := f.clientMap.Get(clientIP)
	if err != nil {
		transport.GetLogger(ctx).WithField("ip", clientIP).Error("client IP not in client map")
		nc.Close()
		return nil, err
	}
	return transport.NewAuthConn(nc, clientIdent), nil
}
