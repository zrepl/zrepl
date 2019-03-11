package dataconn

import (
	"io"
	"sync"
	"time"

	"github.com/zrepl/zrepl/rpc/dataconn/stream"
	"github.com/zrepl/zrepl/zfs"
)

const (
	EndpointPing string = "/v1/ping"
	EndpointSend string = "/v1/send"
	EndpointRecv string = "/v1/recv"
)

const (
	ReqHeader uint32 = 1 + iota
	ReqStructured
	ResHeader
	ResStructured
	ZFSStream
)

// Note that changing theses constants may break interop with other clients
// Aggressive with timing, conservative (future compatible) with buffer sizes
const (
	HeartbeatInterval         = 5 * time.Second
	HeartbeatPeerTimeout      = 10 * time.Second
	RequestHeaderMaxSize      = 1 << 15
	RequestStructuredMaxSize  = 1 << 22
	ResponseHeaderMaxSize     = 1 << 15
	ResponseStructuredMaxSize = 1 << 23
)

// the following are protocol constants
const (
	responseHeaderHandlerOk          = "HANDLER OK\n"
	responseHeaderHandlerErrorPrefix = "HANDLER ERROR:\n"
)

type streamCopier struct {
	mtx                sync.Mutex
	used               bool
	streamConn         *stream.Conn
	closeStreamOnClose bool
}

// WriteStreamTo implements zfs.StreamCopier
func (s *streamCopier) WriteStreamTo(w io.Writer) zfs.StreamCopierError {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if s.used {
		panic("streamCopier used mulitple times")
	}
	s.used = true
	return s.streamConn.ReadStreamInto(w, ZFSStream)
}

// Close implements zfs.StreamCopier
func (s *streamCopier) Close() error {
	// only record the close here, what we do actually depends on whether
	// the streamCopier is instantiated server-side or client-side
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if s.closeStreamOnClose {
		return s.streamConn.Close()
	}
	return nil
}
