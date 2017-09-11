package cmd

import (
	"io"

	"github.com/zrepl/zrepl/rpc"
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

type RPCConnecter interface {
	Connect() (rpc.RPCClient, error)
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
	Prune(fs zfs.DatasetPath, versions []zfs.FilesystemVersion) (keep, remote []zfs.FilesystemVersion, err error)
}
