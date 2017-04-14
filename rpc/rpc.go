package rpc

type RPCRequester interface {
	FilesystemRequest(r FilesystemRequest) (root model.Filesystem, err error)
	InitialTransferRequest(r InitialTransferRequest) (io.Read, error)
	IncrementalTransferRequest(r IncrementalTransferRequest) (io.Read, error)
}

type RPCHandler interface {
	HandleFilesystemRequest(r FilesystemRequest) (root model.Filesystem, err error)
	HandleInitialTransferRequest(r InitialTransferRequest) (io.Read, error)
	HandleIncrementalTransferRequestRequest(r IncrementalTransferRequest) (io.Read, error)
}


type ByteStreamRPC struct {
	conn   io.ReadWriteCloser
}

func ConnectByteStreamRPC(conn io.ReadWriteCloser) (ByteStreamRPC, error) {
	// TODO do ssh connection to transport, establish TCP-like communication channel
	conn := sshtransport.New()
	rpc := ByteStreamRPC{
		conn: conn,
	}
	return conn, nil
}

func ListenByteStreamRPC(conn io.ReadWriteCloser, handler RPCHandler) (error) {
	// Read from connection, decode wire protocol, route requests to handler
	return nil
}

func (c ByteStreamRPC) FilesystemRequest(r FilesystemRequest) (roots []model.Filesystem, err error) {
	encodedReader := protobuf.Encode(r)
	c.conn.Write(NewChunker(encodedReader))
	encodedResponseReader := NewUnchunker(c.conn.Read())
	roots = protobuf.Decode(encodedResponse)
	return
}

func (c ByteStreamRPC) InitialTransferRequest(r InitialTransferRequest) (io.Read, error) {
	// send request header using protobuf or similar
	encodedReader := protobuf.Encode(r)
	c.conn.Write(NewChunker(encodedReader))
	// expect chunked response -> use unchunker on c.conn to read snapshot stream
	return NewUmainnchunker(c.conn.Read())
}

func (c ByteStreamRPC) IncrementalTransferRequest(r IncrementalTransferRequest) (io.Read, error) {

}


type LocalRPC struct {
	handler RPCHandler
}


func ConnectLocalRPC(handler RPCHandler) LocalRPC {
	return LocalRPC{handler}
}

func (c LocalRPC) FilesystemRequest(r FilesystemRequest) (root model.Filesystem, err error) {
	return c.handler.HandleFilesystemRequest(r)
}

func (c LocalRPC) InitialTransferRequest(r InitialTransferRequest) (io.Read, error) {
	return c.handler.HandleInitialTransferRequest(r)
}

func (c LocalRPC) IncrementalTransferRequest(r IncrementalTransferRequest) (io.Read, error) {
	return c.handler.HandleIncrementalTransferRequest(r)
}


