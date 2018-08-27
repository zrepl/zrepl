package helpers

import (
	"github.com/pkg/errors"
	"net"
	"os"
	"path/filepath"
)

func PreparePrivateSockpath(sockpath string) error {
	sockdir := filepath.Dir(sockpath)
	sdstat, err := os.Stat(sockdir)
	if err != nil {
		return errors.Wrapf(err, "cannot stat(2) '%s'", sockdir)
	}
	if !sdstat.IsDir() {
		return errors.Errorf("not a directory: %s", sockdir)
	}
	p := sdstat.Mode().Perm()
	if p&0007 != 0 {
		return errors.Errorf("socket directory must not be world-accessible: %s (permissions are %#o)", sockdir, p)
	}

	// Maybe things have not been cleaned up before
	s, err := os.Stat(sockpath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return errors.Wrapf(err, "cannot stat(2) '%s'", sockpath)
	}
	if s.Mode()&os.ModeSocket == 0 {
		return errors.Errorf("unexpected file type at path '%s'", sockpath)
	}
	err = os.Remove(sockpath)
	if err != nil {
		return errors.Wrapf(err, "cannot remove presumably stale socket '%s'", sockpath)
	}
	return nil
}

func ListenUnixPrivate(sockaddr *net.UnixAddr) (*net.UnixListener, error) {

	if err := PreparePrivateSockpath(sockaddr.Name); err != nil {
		return nil, err
	}

	return net.ListenUnix("unix", sockaddr)
}
