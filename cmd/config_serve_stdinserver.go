package cmd

import (
	"github.com/ftrvxmtrx/fd"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"io"
	"net"
	"os"
	"path"
)

type StdinserverListenerFactory struct {
	ClientIdentity string `mapstructure:"client_identity"`
	sockaddr       *net.UnixAddr
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

	f.sockaddr, err = stdinserverListenerSocket(c.Global.Serve.Stdinserver.SockDir, f.ClientIdentity)
	if err != nil {
		return
	}

	return
}

func stdinserverListenerSocket(sockdir, clientIdentity string) (addr *net.UnixAddr, err error) {
	sockpath := path.Join(sockdir, clientIdentity)
	addr, err = net.ResolveUnixAddr("unix", sockpath)
	if err != nil {
		return nil, errors.Wrap(err, "cannot resolve unix address")
	}
	return addr, nil
}

func (f *StdinserverListenerFactory) Listen() (al AuthenticatedChannelListener, err error) {

	ul, err := ListenUnixPrivate(f.sockaddr)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot listen on unix socket %s", f.sockaddr)
	}

	l := &StdinserverListener{ul}

	return l, nil
}

type StdinserverListener struct {
	l *net.UnixListener
}

type fdRWC struct {
	stdin, stdout *os.File
	control       *net.UnixConn
}

func (f fdRWC) Read(p []byte) (n int, err error) {
	return f.stdin.Read(p)
}

func (f fdRWC) Write(p []byte) (n int, err error) {
	return f.stdout.Write(p)
}

func (f fdRWC) Close() (err error) {
	f.stdin.Close()
	f.stdout.Close()
	return f.control.Close()
}

func (l *StdinserverListener) Accept() (ch io.ReadWriteCloser, err error) {
	c, err := l.l.Accept()
	if err != nil {
		err = errors.Wrap(err, "error accepting on unix listener")
		return
	}

	// Read the stdin and stdout of the stdinserver command
	files, err := fd.Get(c.(*net.UnixConn), 2, []string{"stdin", "stdout"})
	if err != nil {
		err = errors.Wrap(err, "error receiving fds from stdinserver command")
		c.Close()
	}

	rwc := fdRWC{files[0], files[1], c.(*net.UnixConn)}

	return rwc, nil

}

func (l *StdinserverListener) Close() (err error) {
	return l.l.Close() // removes socket file automatically
}
