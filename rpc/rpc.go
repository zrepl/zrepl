package rpc

import (
	"encoding/json"
	"io"

	. "github.com/zrepl/zrepl/model"
	. "github.com/zrepl/zrepl/util"
)

type RPCRequester interface {
	FilesystemRequest(r FilesystemRequest) (root Filesystem, err error)
	InitialTransferRequest(r InitialTransferRequest) (io.Reader, error)
	IncrementalTransferRequest(r IncrementalTransferRequest) (io.Reader, error)
}

type RPCHandler interface {
	HandleFilesystemRequest(r FilesystemRequest) (root Filesystem, err error)
	HandleInitialTransferRequest(r InitialTransferRequest) (io.Reader, error)
	HandleIncrementalTransferRequest(r IncrementalTransferRequest) (io.Reader, error)
}

const ByteStreamRPCProtocolVersion = 1

type ByteStreamRPC struct {
	conn    io.ReadWriteCloser
	encoder *json.Encoder
	decoder *json.Decoder
}

func ConnectByteStreamRPC(conn io.ReadWriteCloser) (ByteStreamRPC, error) {
	// TODO do ssh connection to transport, establish TCP-like communication channel
	rpc := ByteStreamRPC{
		conn:    conn,
		encoder: json.NewEncoder(conn),
		decoder: json.NewDecoder(conn),
	}

	// Assert protocol versions are equal
	req := NewByteStreamRPCProtocolVersionRequest()
	rpc.encoder.Encode(&req)
	return rpc, nil
}

func ListenByteStreamRPC(conn io.ReadWriteCloser, handler RPCHandler) error {

	// A request consists of two subsequent JSON objects
	// Object 1: RequestHeader => contains type of Request Body
	// Object 2: RequestBody, e.g. IncrementalTransferRequest
	// A response is always a ResponseHeader followed by bytes to be interpreted
	// 	as indicated by the ResponseHeader.ResponseType, e.g.
	// 	 a) a chunked response
	//   b) or another JSON object

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	for {

		var header RequestHeader = RequestHeader{}
		if err := decoder.Decode(&header); err != nil {
			respondWithError(conn, EDecodeHeader, err)
			conn.Close()
			return err
		}

		switch header.Type {
		case RTProtocolVersionRequest:
			var rq ByteStreamRPCProtocolVersionRequest
			if err := decoder.Decode(&rq); err != nil {
				respondWithError(conn, EDecodeRequestBody, nil)
				conn.Close()
			}

			if rq.ClientVersion != ByteStreamRPCProtocolVersion {
				respondWithError(conn, EProtocolVersionMismatch, nil)
				conn.Close()
			}

			r := ResponseHeader{
				RequestId: header.Id,
			}
			if err := encoder.Encode(&r); err != nil {
				return err
			}

		case RTFilesystemRequest:
			var rq FilesystemRequest
			if err := decoder.Decode(&rq); err != nil {
				respondWithError(conn, EDecodeRequestBody, nil)
				conn.Close()
			}

			roots, err := handler.HandleFilesystemRequest(rq)
			if err != nil {
				respondWithError(conn, EHandler, err)
			} else {
				if err := encoder.Encode(&roots); err != nil {
					return err
				}
			}

		case RTInitialTransferRequest:
			var rq InitialTransferRequest
			if err := decoder.Decode(&rq); err != nil {
				respondWithError(conn, EDecodeRequestBody, nil)
			}

			snapReader, err := handler.HandleInitialTransferRequest(rq)
			if err != nil {
				respondWithError(conn, EHandler, err)
			} else {
				chunker := NewChunker(snapReader)
				_, err := io.Copy(conn, &chunker)
				if err != nil {
					return err
				}
			}
		// TODO
		default:
			respondWithError(conn, EUnknownRequestType, nil)
			conn.Close()
		}
	}

	return nil
}

func respondWithError(conn io.Writer, id ErrorId, err error) error {
	return nil
}

func (c ByteStreamRPC) FilesystemRequest(r FilesystemRequest) (roots []Filesystem, err error) {
	return nil, nil
}

func (c ByteStreamRPC) InitialTransferRequest(r InitialTransferRequest) (io.Reader, error) {
	// send request header using protobuf or similar
	return nil, nil
}

func (c ByteStreamRPC) IncrementalTransferRequest(r IncrementalTransferRequest) (io.Reader, error) {
	return nil, nil
}

type LocalRPC struct {
	handler RPCHandler
}

func ConnectLocalRPC(handler RPCHandler) LocalRPC {
	return LocalRPC{handler}
}

func (c LocalRPC) FilesystemRequest(r FilesystemRequest) (root Filesystem, err error) {
	return c.handler.HandleFilesystemRequest(r)
}

func (c LocalRPC) InitialTransferRequest(r InitialTransferRequest) (io.Reader, error) {
	return c.handler.HandleInitialTransferRequest(r)
}

func (c LocalRPC) IncrementalTransferRequest(r IncrementalTransferRequest) (reader io.Reader, err error) {
	reader, err = c.handler.HandleIncrementalTransferRequest(r)
	return
}
