package common

import (
	"context"
	"io"

	"github.com/zrepl/zrepl/cmd/replication/pdu"
	"github.com/zrepl/zrepl/logger"
)

type contextKey int

const (
	contextKeyLog contextKey = iota
)

type Logger = logger.Logger

func ContextWithLogger(ctx context.Context, l Logger) context.Context {
	return context.WithValue(ctx, contextKeyLog, l)
}

func GetLogger(ctx context.Context) Logger {
	l, ok := ctx.Value(contextKeyLog).(Logger)
	if !ok {
		l = logger.NewNullLogger()
	}
	return l
}

type ReplicationEndpoint interface {
	// Does not include placeholder filesystems
	ListFilesystems(ctx context.Context) ([]*pdu.Filesystem, error)
	ListFilesystemVersions(ctx context.Context, fs string) ([]*pdu.FilesystemVersion, error) // fix depS
	Send(ctx context.Context, r *pdu.SendReq) (*pdu.SendRes, io.ReadCloser, error)
	Receive(ctx context.Context, r *pdu.ReceiveReq, sendStream io.ReadCloser) error
}

type FilteredError struct{ fs string }

func NewFilteredError(fs string) *FilteredError {
	return &FilteredError{fs}
}

func (f FilteredError) Error() string { return "endpoint does not allow access to filesystem " + f.fs }
