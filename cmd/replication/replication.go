package replication

import (
	"context"
	"io"
	"container/list"
	"fmt"
	"net"
)

type ReplicationEndpoint interface {
	// Does not include placeholder filesystems
	ListFilesystems(ctx context.Context) ([]*Filesystem, error)
	ListFilesystemVersions(ctx context.Context, fs string) ([]*FilesystemVersion, error) // fix depS
	Sender
	Receiver
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
	ContextKeyLog contextKey = iota
)

type Logger interface{
	Printf(fmt string, args ... interface{})
}

type replicationWork struct {
	fs            *Filesystem
}

type FilesystemReplicationResult struct {
	Done      bool
	Retry 	  bool
	Unfixable bool
}

func handleGenericEndpointError(err error) FilesystemReplicationResult {
	if _, ok := err.(net.Error); ok {
		return FilesystemReplicationResult{Retry: true}
	}
	return FilesystemReplicationResult{Unfixable: true}
}

func driveFSReplication(ctx context.Context, ws *list.List, allTriedOnce chan struct{}, log Logger, f func(w *replicationWork) FilesystemReplicationResult) {
	initialLen, fCalls := ws.Len(), 0
	for ws.Len() > 0 {
		select {
		case <-ctx.Done():
			log.Printf("aborting replication due to context error: %s", ctx.Err())
			return
		default:
		}

		w := ws.Remove(ws.Front()).(*replicationWork)
		res := f(w)
		fCalls++
		if fCalls == initialLen {
			select {
			case allTriedOnce <- struct{}{}:
			default:
			}
		}
		if res.Done {
			log.Printf("finished replication of %s", w.fs.Path)
			continue
		}

		if res.Unfixable {
			log.Printf("aborting replication of %s after unfixable error", w.fs.Path)
			continue
		}

		log.Printf("queuing replication of %s for retry", w.fs.Path)
		ws.PushBack(w)
	}
}

func resolveConflict(conflict error) (path []*FilesystemVersion, msg string) {
	if noCommonAncestor, ok := conflict.(*ConflictNoCommonAncestor); ok {
		if len(noCommonAncestor.SortedReceiverVersions) == 0 {
			// FIXME hard-coded replication policy: most recent
			// snapshot as source
			var mostRecentSnap *FilesystemVersion
			for n := len(noCommonAncestor.SortedSenderVersions) -1; n >= 0; n-- {
				if noCommonAncestor.SortedSenderVersions[n].Type == FilesystemVersion_Snapshot {
					mostRecentSnap = noCommonAncestor.SortedSenderVersions[n]
					break
				}
			}
			if mostRecentSnap == nil {
				return nil, "no snapshots available on sender side"
			}
			return []*FilesystemVersion{mostRecentSnap}, fmt.Sprintf("start replication at most recent snapshot %s", mostRecentSnap)
		}
	}
	return nil, "no automated way to handle conflict type"
}

// Replicate replicates filesystems from ep.Sender() to ep.Receiver().
//
// All filesystems presented by the sending side are replicated,
// unless the receiver rejects a Receive request with a *FilteredError.
//
// If an error occurs when replicating a filesystem, that error is logged to the logger in ctx.
// Replicate continues with the replication of the remaining file systems.
// Depending on the type of error, failed replications are retried in an unspecified order (currently FIFO).
func Replicate(ctx context.Context, ep EndpointPair, ipr IncrementalPathReplicator, allTriedOnce chan struct{}) {

	log := ctx.Value(ContextKeyLog).(Logger)

	sfss, err := ep.Sender().ListFilesystems(ctx)
	if err != nil {
		log.Printf("error listing sender filesystems: %s", err)
		return
	}

	rfss, err := ep.Receiver().ListFilesystems(ctx)
	if err != nil {
		log.Printf("error listing receiver filesystems: %s", err)
		return
	}

	wq := list.New()
	for _, fs := range sfss {
		wq.PushBack(&replicationWork{
			fs:            fs,
		})
	}

	driveFSReplication(ctx, wq, allTriedOnce, log, func(w *replicationWork) FilesystemReplicationResult {
		fs := w.fs

		log.Printf("replicating %s", fs.Path)

		sfsvs, err := ep.Sender().ListFilesystemVersions(ctx, fs.Path)
		if err != nil {
			log.Printf("cannot get remote filesystem versions: %s", err)
			return handleGenericEndpointError(err)
		}

		if len(sfsvs) <= 1 {
			log.Printf("sender does not have any versions")
			return FilesystemReplicationResult{Unfixable: true}
		}

		receiverFSExists := false
		for _, rfs := range rfss {
			if rfs.Path == fs.Path {
				receiverFSExists = true
			}
		}

		var rfsvs []*FilesystemVersion
		if receiverFSExists {
			rfsvs, err = ep.Receiver().ListFilesystemVersions(ctx, fs.Path)
			if err != nil {
				if _, ok := err.(FilteredError); ok {
					log.Printf("receiver does not map %s", fs.Path)
					return FilesystemReplicationResult{Done: true}
				}
				log.Printf("receiver error %s", err)
				return handleGenericEndpointError(err)
			}
		} else {
			rfsvs = []*FilesystemVersion{}
		}

		path, conflict := IncrementalPath(rfsvs, sfsvs)
		if conflict != nil {
			log.Printf("conflict: %s", conflict)
			var msg string
			path, msg = resolveConflict(conflict)
			if path != nil {
				log.Printf("conflict resolved: %s", msg)
			} else {
				log.Printf("%s", msg)
			}
		}
		if path == nil {
			return FilesystemReplicationResult{Unfixable: true}
		}

		return ipr.Replicate(ctx, ep.Sender(), ep.Receiver(), NewCopier(), fs, path)

	})

}

type Sender interface {
	Send(ctx context.Context, r *SendReq) (*SendRes, io.ReadCloser, error)
}

type Receiver interface {
	Receive(ctx context.Context, r *ReceiveReq, sendStream io.ReadCloser) (error)
}

type Copier interface {
	Copy(writer io.Writer, reader io.Reader) (int64, error)
}

type copier struct{}

func (copier) Copy(writer io.Writer, reader io.Reader) (int64, error) {
	return io.Copy(writer, reader)
}

func NewCopier() Copier {
	return copier{}
}

type IncrementalPathReplicator interface {
	Replicate(ctx context.Context, sender Sender, receiver Receiver, copier Copier, fs *Filesystem, path []*FilesystemVersion) FilesystemReplicationResult
}

type incrementalPathReplicator struct{}

func NewIncrementalPathReplicator() IncrementalPathReplicator {
	return incrementalPathReplicator{}
}

func (incrementalPathReplicator) Replicate(ctx context.Context, sender Sender, receiver Receiver, copier Copier, fs *Filesystem, path []*FilesystemVersion) FilesystemReplicationResult {

	log := ctx.Value(ContextKeyLog).(Logger)

	if len(path) == 0 {
		log.Printf("nothing to do")
		return FilesystemReplicationResult{Done: true}
	}

	if len(path) == 1 {
		log.Printf("full send of version %s", path[0])

		sr := &SendReq{
			Filesystem: fs.Path,
			From: path[0].RelName(),
			ResumeToken: fs.ResumeToken,
		}
		sres, sstream, err := sender.Send(ctx, sr)
		if err != nil {
			log.Printf("send request failed: %s", err)
			return handleGenericEndpointError(err)
		}

		rr := &ReceiveReq{
			Filesystem: fs.Path,
			ClearResumeToken: fs.ResumeToken != "" && !sres.UsedResumeToken,
		}
		err = receiver.Receive(ctx, rr, sstream)
		if err != nil {
			log.Printf("receive request failed (might also be error on sender): %s", err)
			sstream.Close()
			// This failure could be due to
			// 	- an unexpected exit of ZFS on the sending side
			//  - an unexpected exit of ZFS on the receiving side
			//  - a connectivity issue
			return handleGenericEndpointError(err)
		}
		return FilesystemReplicationResult{Done: true}
	}

	usedResumeToken := false

	for j := 0; j < len(path)-1; j++ {
		rt := ""
		if !usedResumeToken { // only send resume token for first increment
			rt = fs.ResumeToken
			usedResumeToken = true
		}
		sr := &SendReq{
			Filesystem:  fs.Path,
			From:        path[j].RelName(),
			To:          path[j+1].RelName(),
			ResumeToken: rt,
		}
		sres, sstream, err := sender.Send(ctx, sr)
		if err != nil {
			log.Printf("send request failed: %s", err)
			return handleGenericEndpointError(err)
		}
		// try to consume stream

		rr := &ReceiveReq{
			Filesystem:  fs.Path,
			ClearResumeToken: rt != "" && !sres.UsedResumeToken,
		}
		err = receiver.Receive(ctx, rr, sstream)
		if err != nil {
			log.Printf("receive request failed: %s", err)
			return handleGenericEndpointError(err) // FIXME resume state on receiver -> update ResumeToken
		}

		// FIXME handle properties from sres
	}

	return FilesystemReplicationResult{Done: true}
}
