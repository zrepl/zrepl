package cmd

import (
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/problame/go-netssh"
	"io"
	"path"
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

func (f *StdinserverListenerFactory) Listen() (al AuthenticatedChannelListener, err error) {

	if err = PreparePrivateSockpath(f.sockpath); err != nil {
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

func (l StdinserverListener) Accept() (ch io.ReadWriteCloser, err error) {
	return l.l.Accept()
}

func (l StdinserverListener) Close() (err error) {
	return l.l.Close()
}
