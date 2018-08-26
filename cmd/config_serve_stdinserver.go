package cmd

import (
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/problame/go-netssh"
	"net"
	"path"
	"github.com/zrepl/zrepl/cmd/helpers"
)

type StdinserverListenerFactory struct {
	ClientIdentity string `mapstructure:"client_identity"`
	sockpath       string
}

func parseStdinserverListenerFactory(c JobParsingContext, i map[string]interface{}) (f *StdinserverListenerFactory, err error) {

	f = &StdinserverListenerFactory{}

	if err = mapstructure.Decode(i, f); err != nil {
		return nil, errors.Wrap(err, "mapstructure error")
	}
	if !(len(f.ClientIdentity) > 0) {
		err = errors.Errorf("must specify 'client_identity'")
		return
	}

	f.sockpath = path.Join(c.Global.Serve.Stdinserver.SockDir, f.ClientIdentity)

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
