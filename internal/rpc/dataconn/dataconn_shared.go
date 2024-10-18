package dataconn

import (
	"time"
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
