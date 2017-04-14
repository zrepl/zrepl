package protocol

type RequestId uint8
type Request struct {
	RequestType RequestType
	RequestId 	[16]byte // UUID
}

type FilesystemRequest struct {
	Request
	Roots []string
}

type InitialTransferRequest struct {
	Request
	Snapshot string // tank/my/db@ljlsdjflksdf
}
func (r InitialTransferRequest) Respond(snapshotReader io.Reader) {

}

type IncrementalTransferRequest struct {
	Request
	FromSnapshot string
	ToSnapshot 	 string
}
func (r IncrementalTransferRequest) Respond(snapshotReader io.Reader) {

}
