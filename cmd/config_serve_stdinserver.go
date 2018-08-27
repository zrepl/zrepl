package cmd

import (
	"github.com/problame/go-netssh"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/cmd/helpers"
	"net"
	"path"
)

type StdinserverListenerFactory struct {
	ClientIdentity string
	sockpath       string
}

func parseStdinserverListenerFactory(c config.Global, in config.StdinserverServer) (f *StdinserverListenerFactory, err error) {

	f = &StdinserverListenerFactory{
		ClientIdentity: in.ClientIdentity,
	}

	f.sockpath = path.Join(c.Serve.StdinServer.SockDir, f.ClientIdentity)

	return
}

func (f *StdinserverListenerFactory) Listen() (net.Listener, error) {

	if err := helpers.PreparePrivateSockpath(f.sockpath); err != nil {
		return nil, err
	}

	l, err := netssh.Listen(f.sockpath)
	if err != nil {
		return nil, err
	}
	return StdinserverListener{l}, nil
}

type StdinserverListener struct {
	l *netssh.Listener
}

func (l StdinserverListener) Addr() net.Addr {
	return netsshAddr{}
}

func (l StdinserverListener) Accept() (net.Conn, error) {
	c, err := l.l.Accept()
	if err != nil {
		return nil, err
	}
	return netsshConnToNetConnAdatper{c}, nil
}

func (l StdinserverListener) Close() (err error) {
	return l.l.Close()
}
