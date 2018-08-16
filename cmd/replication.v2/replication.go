package replication

import (
	"os"
	"syscall"
	"encoding/json"
	"context"
	"fmt"
	"github.com/zrepl/zrepl/logger"
	"io"
	"os/signal"
)

type ReplicationEndpoint interface {
	// Does not include placeholder filesystems
	ListFilesystems(ctx context.Context) ([]*Filesystem, error)
	ListFilesystemVersions(ctx context.Context, fs string) ([]*FilesystemVersion, error) // fix depS
	Send(ctx context.Context, r *SendReq) (*SendRes, io.ReadCloser, error)
	Receive(ctx context.Context, r *ReceiveReq, sendStream io.ReadCloser) error
}

type FilteredError struct{ fs string }

func NewFilteredError(fs string) FilteredError {
	return FilteredError{fs}
}

func (f FilteredError) Error() string { return "endpoint does not allow access to filesystem " + f.fs }

type ReplicationMode int

const (
	ReplicationModePull ReplicationMode = iota
	ReplicationModePush
)

type EndpointPair struct {
	a, b ReplicationEndpoint
	m    ReplicationMode
}

func NewEndpointPairPull(sender, receiver ReplicationEndpoint) EndpointPair {
	return EndpointPair{sender, receiver, ReplicationModePull}
}

func NewEndpointPairPush(sender, receiver ReplicationEndpoint) EndpointPair {
	return EndpointPair{receiver, sender, ReplicationModePush}
}

func (p EndpointPair) Sender() ReplicationEndpoint {
	switch p.m {
	case ReplicationModePull:
		return p.a
	case ReplicationModePush:
		return p.b
	}
	panic("should not be reached")
	return nil
}

func (p EndpointPair) Receiver() ReplicationEndpoint {
	switch p.m {
	case ReplicationModePull:
		return p.b
	case ReplicationModePush:
		return p.a
	}
	panic("should not be reached")
	return nil
}

func (p EndpointPair) Mode() ReplicationMode {
	return p.m
}

type contextKey int

const (
	contextKeyLog contextKey = iota
)

//type Logger interface {
//	Infof(fmt string, args ...interface{})
//	Errorf(fmt string, args ...interface{})
//}

//var _ Logger = nullLogger{}

//type nullLogger struct{}
//
//func (nullLogger) Infof(fmt string, args ...interface{}) {}
//func (nullLogger) Errorf(fmt string, args ...interface{}) {}

type Logger = logger.Logger

func ContextWithLogger(ctx context.Context, l Logger) context.Context {
	return context.WithValue(ctx, contextKeyLog, l)
}

func getLogger(ctx context.Context) Logger {
	l, ok := ctx.Value(contextKeyLog).(Logger)
	if !ok {
		l = logger.NewNullLogger()
	}
	return l
}

func resolveConflict(conflict error) (path []*FilesystemVersion, msg string) {
	if noCommonAncestor, ok := conflict.(*ConflictNoCommonAncestor); ok {
		if len(noCommonAncestor.SortedReceiverVersions) == 0 {
			// FIXME hard-coded replication policy: most recent
			// snapshot as source
			var mostRecentSnap *FilesystemVersion
			for n := len(noCommonAncestor.SortedSenderVersions) - 1; n >= 0; n-- {
				if noCommonAncestor.SortedSenderVersions[n].Type == FilesystemVersion_Snapshot {
					mostRecentSnap = noCommonAncestor.SortedSenderVersions[n]
					break
				}
			}
			if mostRecentSnap == nil {
				return nil, "no snapshots available on sender side"
			}
			return []*FilesystemVersion{mostRecentSnap}, fmt.Sprintf("start replication at most recent snapshot %s", mostRecentSnap.RelName())
		}
	}
	return nil, "no automated way to handle conflict type"
}

func NewReplication() *Replication {
	r := Replication{
		state: Planning,
	}
	return &r
}
// Replicate replicates filesystems from ep.Sender() to ep.Receiver().
//
// All filesystems presented by the sending side are replicated,
// unless the receiver rejects a Receive request with a *FilteredError.
//
// If an error occurs when replicating a filesystem, that error is logged to the logger in ctx.
// Replicate continues with the replication of the remaining file systems.
// Depending on the type of error, failed replications are retried in an unspecified order (currently FIFO).
func Replicate(ctx context.Context, ep EndpointPair, retryNow chan struct{}) {
	r := Replication{
		state: Planning,
	}

	c := make(chan os.Signal)
	defer close(c)
	signal.Notify(c, syscall.SIGHUP)
	go func() {
		f, err := os.OpenFile("/tmp/report", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			getLogger(ctx).WithError(err).Error("cannot open report file")
			panic(err)
		}
		defer f.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case sig := <-c:
				if sig == nil {
					return
				}
				report := r.Report()
				enc := json.NewEncoder(f)
				enc.SetIndent("  ", "  ")
				if err := enc.Encode(report); err != nil {
					getLogger(ctx).WithError(err).Error("cannot encode report")
					panic(err)
				}
				f.Write([]byte("\n"))
				f.Sync()
			}
		}
	}()

	r.Drive(ctx, ep, retryNow)
}

