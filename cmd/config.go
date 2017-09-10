package cmd

import (
	"io"

	"github.com/zrepl/zrepl/rpc"
	"github.com/zrepl/zrepl/zfs"
)

type Config struct {
	Jobs map[string]Job
}

type RPCConnecter interface {
	Connect() (rpc.RPCClient, error)
}
type AuthenticatedChannelListenerFactory interface {
	Listen() AuthenticatedChannelListener
}

type AuthenticatedChannelListener interface {
	Accept() (ch io.ReadWriteCloser, err error)
}

type SSHStdinServerConnectDescr struct {
}

type PrunePolicy interface {
	Prune(fs zfs.DatasetPath, versions []zfs.FilesystemVersion) (keep, remote []zfs.FilesystemVersion, err error)
}

