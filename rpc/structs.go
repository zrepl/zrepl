package rpc

import "io"
import "github.com/zrepl/zrepl/zfs"

type RequestId [16]byte
type RequestType uint8

const (
	RTProtocolVersionRequest     RequestType = 0x01
	RTFilesystemRequest                      = 0x10
	RTFilesystemVersionsRequest              = 0x11
	RTInitialTransferRequest                 = 0x12
	RTIncrementalTransferRequest             = 0x13
	RTCloseRequest                           = 0x20
)

type RequestHeader struct {
	Type RequestType
	Id   [16]byte // UUID
}

type FilesystemRequest struct {
	Roots []string // may be nil, indicating interest in all filesystems
}

type FilesystemVersionsRequest struct {
	Filesystem zfs.DatasetPath
}

type InitialTransferRequest struct {
	Filesystem        zfs.DatasetPath
	FilesystemVersion zfs.FilesystemVersion
}

func (r InitialTransferRequest) Respond(snapshotReader io.Reader) {

}

type IncrementalTransferRequest struct {
	Filesystem zfs.DatasetPath
	From       zfs.FilesystemVersion
	To         zfs.FilesystemVersion
}

func (r IncrementalTransferRequest) Respond(snapshotReader io.Reader) {

}

type ByteStreamRPCProtocolVersionRequest struct {
	ClientVersion uint8
}

const LOCAL_TRANSPORT_IDENTITY string = "local"

const DEFAULT_INITIAL_REPL_POLICY = InitialReplPolicyMostRecent

type InitialReplPolicy string

const (
	InitialReplPolicyMostRecent InitialReplPolicy = "most_recent"
	InitialReplPolicyAll        InitialReplPolicy = "all"
)

type CloseRequest struct {
	Goodbye string
}

type ErrorId uint8

const (
	ENoError                 ErrorId = 0
	EDecodeHeader                    = 1
	EUnknownRequestType              = 2
	EDecodeRequestBody               = 3
	EProtocolVersionMismatch         = 4
	EHandler                         = 5
)

type ResponseType uint8

const (
	RNONE           ResponseType = 0x0
	ROK                          = 0x1
	RFilesystems                 = 0x10
	RFilesystemDiff              = 0x11
	RChunkedStream               = 0x20
)

type ResponseHeader struct {
	RequestId    RequestId
	ErrorId      ErrorId
	Message      string
	ResponseType ResponseType
}

func NewByteStreamRPCProtocolVersionRequest() ByteStreamRPCProtocolVersionRequest {
	return ByteStreamRPCProtocolVersionRequest{
		ClientVersion: ByteStreamRPCProtocolVersion,
	}
}

func newUUID() [16]byte {
	return [16]byte{}
}
