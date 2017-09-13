package cmd

import (
	"io"

	"github.com/zrepl/zrepl/zfs"
)

type Config struct {
	Global Global
	Jobs   map[string]Job
}

type Global struct {
	Serve struct {
		Stdinserver struct {
			SockDir string
		}
	}
}

type JobDebugSettings struct {
	Conn struct {
		ReadDump  string `mapstructure:"read_dump"`
		WriteDump string `mapstructure:"write_dump"`
	}
	RPC struct {
		Log bool
	}
}

type RWCConnecter interface {
	Connect() (io.ReadWriteCloser, error)
}
type AuthenticatedChannelListenerFactory interface {
	Listen() (AuthenticatedChannelListener, error)
}

type AuthenticatedChannelListener interface {
	Accept() (ch io.ReadWriteCloser, err error)
	Close() (err error)
}

type SSHStdinServerConnectDescr struct {
}

type PrunePolicy interface {
	Prune(fs *zfs.DatasetPath, versions []zfs.FilesystemVersion) (keep, remove []zfs.FilesystemVersion, err error)
}
