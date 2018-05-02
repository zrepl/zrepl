package replication

import (
	"context"
	"github.com/zrepl/zrepl/zfs"
	"io"
)

type ReplicationEndpoint interface {
	// Does not include placeholder filesystems
	ListFilesystems() ([]Filesystem, error)
	ListFilesystemVersions(fs string) ([]zfs.FilesystemVersion, error) // fix depS
	Sender
	Receiver
}

type Filesystem struct {
	Path        string
	ResumeToken string
}

type FilteredError struct{ fs string }

func (f FilteredError) Error() string { return "endpoint does not allow access to filesystem " + f.fs }

type SendRequest struct {
	Filesystem string
	From, To   string
	// If ResumeToken is not empty, the resume token that CAN be tried for 'zfs send' by the sender
	// If it does not work, the sender SHOULD clear the resume token on their side
	// and use From and To instead
	// If ResumeToken is not empty, the GUIDs of From and To
	// MUST correspond to those encoded in the ResumeToken.
	// Otherwise, the Sender MUST return an error.
	ResumeToken string
	Compress    bool
	Dedup       bool
}

type SendResponse struct {
	Properties zfs.ZFSProperties // fix dep
	Stream     io.Reader
}

type ReceiveRequest struct {
	Filesystem string
	// The resume token used by the sending side.
	// The receiver MUST discard the saved state on their side if ResumeToken
	// does not match the zfs property of Filesystem on their side.
	ResumeToken string
}

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

func Replicate(ctx context.Context, ep EndpointPair, ipr IncrementalPathReplicator) {

	sfss, err := ep.Sender().ListFilesystems()
	if err != nil {
		// log error
		return
	}

	for _, fs := range sfss {
		sfsvs, err := ep.Sender().ListFilesystemVersions(fs.Path)
		rfsvs, err := ep.Receiver().ListFilesystemVersions(fs.Path)
		if err != nil {
			if _, ok := err.(FilteredError); ok {
				// Remote does not map filesystem, don't try to tx it
				continue
			}
			// log and ignore
			continue
		}

		path, conflict := IncrementalPath(rfsvs, sfsvs)
		if conflict != nil {
			// handle or ignore for now
			continue
		}

		ipr.Replicate(ctx, ep.Sender(), ep.Receiver(), NewCopier(), fs, path)

	}

}

type Sender interface {
	Send(r SendRequest) (SendResponse, error)
}

type Receiver interface {
	Receive(r ReceiveRequest) (io.Writer, error)
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
	Replicate(ctx context.Context, sender Sender, receiver Receiver, copier Copier, fs Filesystem, path []zfs.FilesystemVersion)
}

type incrementalPathReplicator struct{}

func NewIncrementalPathReplicator() IncrementalPathReplicator {
	return incrementalPathReplicator{}
}

func (incrementalPathReplicator) Replicate(ctx context.Context, sender Sender, receiver Receiver, copier Copier, fs Filesystem, path []zfs.FilesystemVersion) {

	if len(path) == 0 {
		// nothing to do
		return
	}

	usedResumeToken := false

incrementalLoop:
	for j := 0; j < len(path)-1; j++ {
		rt := ""
		if !usedResumeToken {
			rt = fs.ResumeToken
			usedResumeToken = true
		}
		sr := SendRequest{
			Filesystem:  fs.Path,
			From:        path[j].String(),
			To:          path[j+1].String(),
			ResumeToken: rt,
		}
		sres, err := sender.Send(sr)
		if err != nil {
			// handle and ignore
			break incrementalLoop
		}
		// try to consume stream

		rr := ReceiveRequest{
			Filesystem:  fs.Path,
			ResumeToken: rt,
		}
		recvWriter, err := receiver.Receive(rr)
		if err != nil {
			// handle and ignore
			break incrementalLoop
		}
		_, err = copier.Copy(recvWriter, sres.Stream)
		if err != nil {
			// handle and ignore
			break incrementalLoop
		}

		// handle properties from sres
	}
}
