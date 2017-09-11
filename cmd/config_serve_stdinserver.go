package cmd

import (
	"github.com/ftrvxmtrx/fd"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/util"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
)

type StdinserverListenerFactory struct {
	ClientIdentity   string `mapstructure:"client_identity"`
	ConnLogReadFile  string `mapstructure:"connlog_read_file"`
	ConnLogWriteFile string `mapstructure:"connlog_write_file"`
}

func parseStdinserverListenerFactory(i map[string]interface{}) (f *StdinserverListenerFactory, err error) {

	f = &StdinserverListenerFactory{}

	if err = mapstructure.Decode(i, f); err != nil {
		return nil, errors.Wrap(err, "mapstructure error")
	}
	if !(len(f.ClientIdentity) > 0) {
		err = errors.Errorf("must specify 'client_identity'")
		return
	}
	return
}

func stdinserverListenerSockpath(clientIdentity string) (addr *net.UnixAddr, err error) {
	sockpath := path.Join(conf.Global.Serve.Stdinserver.SockDir, clientIdentity)
	addr, err = net.ResolveUnixAddr("unix", sockpath)
	if err != nil {
		return nil, errors.Wrap(err, "cannot resolve unix address")
	}
	return addr, nil
}

func (f *StdinserverListenerFactory) Listen() (al AuthenticatedChannelListener, err error) {

	unixaddr, err := stdinserverListenerSockpath(f.ClientIdentity)

	sockdir := filepath.Dir(unixaddr.Name)
	sdstat, err := os.Stat(sockdir)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot stat(2) sockdir '%s'", sockdir)
	}
	if !sdstat.IsDir() {
		return nil, errors.Errorf("sockdir is not a directory: %s", sockdir)
	}
	p := sdstat.Mode().Perm()
	if p&0007 != 0 {
		return nil, errors.Errorf("sockdir must not be world-accessible (permissions are %#o)", p)
	}

	ul, err := net.ListenUnix("unix", unixaddr) // TODO
	if err != nil {
		return nil, errors.Wrapf(err, "cannot listen on unix socket %s", unixaddr)
	}

	l := &StdinserverListener{ul, f.ConnLogReadFile, f.ConnLogWriteFile}

	return l, nil
}

type StdinserverListener struct {
	l                *net.UnixListener
	ConnLogReadFile  string `mapstructure:"connlog_read_file"`
	ConnLogWriteFile string `mapstructure:"connlog_write_file"`
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

	rwclog, err := util.NewReadWriteCloserLogger(rwc, l.ConnLogReadFile, l.ConnLogWriteFile)
	if err != nil {
		panic(err)
	}

	return rwclog, nil

}

func (l *StdinserverListener) Close() (err error) {
	return l.l.Close() // removes socket file automatically
}
