package replication

import (
	"context"
	"io"
)

type ReplicationEndpoint interface {
	// Does not include placeholder filesystems
	ListFilesystems() ([]*Filesystem, error)
	ListFilesystemVersions(fs string) ([]*FilesystemVersion, error) // fix depS
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

func Replicate(ctx context.Context, ep EndpointPair, ipr IncrementalPathReplicator) {

	log := ctx.Value(ContextKeyLog).(Logger)

	sfss, err := ep.Sender().ListFilesystems()
	if err != nil {
		log.Printf("error listing sender filesystems: %s", err)
		return
	}

	rfss, err := ep.Receiver().ListFilesystems()
	if err != nil {
		log.Printf("error listing receiver filesystems: %s", err)
		return
	}

	for _, fs := range sfss {
		log.Printf("replication fs %s", fs.Path)
		sfsvs, err := ep.Sender().ListFilesystemVersions(fs.Path)
		if err != nil {
			log.Printf("sender error %s", err)
			continue
		}

		if len(sfsvs) <= 1 {
			log.Printf("sender does not have any versions")
			continue
		}

		receiverFSExists := false
		for _, rfs := range rfss {
			if rfs.Path == fs.Path {
				receiverFSExists = true
			}
		}

		var rfsvs []*FilesystemVersion
		if receiverFSExists {
			rfsvs, err = ep.Receiver().ListFilesystemVersions(fs.Path)
			if err != nil {
				log.Printf("receiver error %s", err)
				if _, ok := err.(FilteredError); ok {
					// Remote does not map filesystem, don't try to tx it
					continue
				}
				// log and ignore
				continue
			}
		} else {
			rfsvs = []*FilesystemVersion{}
		}

		path, conflict := IncrementalPath(rfsvs, sfsvs)
		if noCommonAncestor, ok := conflict.(*ConflictNoCommonAncestor); ok {
			if len(noCommonAncestor.SortedReceiverVersions) == 0 {
				log.Printf("initial replication")
				// FIXME hard-coded replication policy: most recent
				// snapshot as source
				var mostRecentSnap *FilesystemVersion
				for n := len(sfsvs) -1; n >= 0; n-- {
					if sfsvs[n].Type == FilesystemVersion_Snapshot {
						mostRecentSnap = sfsvs[n]
						break
					}
				}
				if mostRecentSnap == nil {
					log.Printf("no snapshot on sender side")
					continue
				}
				log.Printf("starting at most recent snapshot %s", mostRecentSnap)
				path = []*FilesystemVersion{mostRecentSnap}
			}
		} else if conflict != nil {
			log.Printf("unresolvable conflict: %s", conflict)
			// handle or ignore for now
			continue
		}

		ipr.Replicate(ctx, ep.Sender(), ep.Receiver(), NewCopier(), fs, path)

	}

}

type Sender interface {
	Send(r *SendReq) (*SendRes, io.Reader, error)
}

type Receiver interface {
	Receive(r *ReceiveReq, sendStream io.Reader) (error)
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
	Replicate(ctx context.Context, sender Sender, receiver Receiver, copier Copier, fs *Filesystem, path []*FilesystemVersion)
}

type incrementalPathReplicator struct{}

func NewIncrementalPathReplicator() IncrementalPathReplicator {
	return incrementalPathReplicator{}
}

func (incrementalPathReplicator) Replicate(ctx context.Context, sender Sender, receiver Receiver, copier Copier, fs *Filesystem, path []*FilesystemVersion) {

	log := ctx.Value(ContextKeyLog).(Logger)

	if len(path) == 0 {
		log.Printf("nothing to do")
		// nothing to do
		return
	}

	if len(path) == 1 {
		log.Printf("full send of version %s", path[0])

		sr := &SendReq{
			Filesystem: fs.Path,
			From: path[0].RelName(),
			ResumeToken: fs.ResumeToken,
		}
		sres, sstream, err := sender.Send(sr)
		if err != nil {
			log.Printf("send request failed: %s", err)
			// FIXME must close connection...
			return
		}

		rr := &ReceiveReq{
			Filesystem: fs.Path,
			ClearResumeToken: fs.ResumeToken != "" && !sres.UsedResumeToken,
		}
		err = receiver.Receive(rr, sstream)
		if err != nil {
			// FIXME this failure could be due to an unexpected exit of ZFS on the sending side
			// FIXME  which is transported through the streamrpc protocol, and known to the sendStream.(*streamrpc.streamReader),
			// FIXME  but the io.Reader interface design doesn not allow us to infer that it is a *streamrpc.streamReader right now
			log.Printf("receive request failed (might also be error on sender...): %s", err)
			// FIXME must close connection
			return
		}

		return
	}

	usedResumeToken := false

incrementalLoop:
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
		sres, sstream, err := sender.Send(sr)
		if err != nil {
			log.Printf("send request failed: %s", err)
			// handle and ignore
			break incrementalLoop
		}
		// try to consume stream

		rr := &ReceiveReq{
			Filesystem:  fs.Path,
			ClearResumeToken: rt != "" && !sres.UsedResumeToken,
		}
		err = receiver.Receive(rr, sstream)
		if err != nil {
			log.Printf("receive request failed: %s", err)
			// handle and ignore
			break incrementalLoop
		}

		// FIXME handle properties from sres
	}
}
