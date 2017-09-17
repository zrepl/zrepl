package cmd

import (
	"github.com/pkg/errors"
	"net"
	"os"
	"path/filepath"
)

func ListenUnixPrivate(sockaddr *net.UnixAddr) (*net.UnixListener, error) {

	sockdir := filepath.Dir(sockaddr.Name)
	sdstat, err := os.Stat(sockdir)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot stat(2) '%s'", sockdir)
	}
	if !sdstat.IsDir() {
		return nil, errors.Errorf("%s is not a directory: %s", sockdir)
	}
	p := sdstat.Mode().Perm()
	if p&0007 != 0 {
		return nil, errors.Errorf("%s must not be world-accessible (permissions are %#o)", p)
	}

	return net.ListenUnix("unix", sockaddr)
}
