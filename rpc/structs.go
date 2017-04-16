package rpc

import "io"

type RequestId [16]byte
type RequestType uint8

const (
	RTProtocolVersionRequest RequestType = 1
	RTFilesystemRequest                  = 16
	RTInitialTransferRequest             = 17
	RTIncrementalTransferRequest 		 = 18
)

type RequestHeader struct {
	Type RequestType
	Id   [16]byte // UUID
}

type FilesystemRequest struct {
	Roots []string
}

type InitialTransferRequest struct {
	Snapshot string // tank/my/db@ljlsdjflksdf
}

func (r InitialTransferRequest) Respond(snapshotReader io.Reader) {

}

type IncrementalTransferRequest struct {
	FromSnapshot string
	ToSnapshot   string
}

func (r IncrementalTransferRequest) Respond(snapshotReader io.Reader) {

}

type ByteStreamRPCProtocolVersionRequest struct {
	ClientVersion uint8
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
	ROK ResponseType = 1
	RFilesystems     = 2
	RChunkedStream   = 3
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
