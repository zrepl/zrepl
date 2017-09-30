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
		return nil, errors.Errorf("not a directory: %s", sockdir)
	}
	p := sdstat.Mode().Perm()
	if p&0007 != 0 {
		return nil, errors.Errorf("socket directory not be world-accessible: %s (permissions are %#o)", sockdir, p)
	}

	// Maybe things have not been cleaned up before
	s, err := os.Stat(sockaddr.Name)
	if err == nil {
		if s.Mode()&os.ModeSocket != 0 {
			// opportunistically try to remove it, but if this fails, it is not an error
			os.Remove(sockaddr.Name)
		}
	}

	return net.ListenUnix("unix", sockaddr)
}
