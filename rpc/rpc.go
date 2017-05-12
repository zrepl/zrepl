package rpc

import (
	"encoding/json"
	"errors"
	"fmt"
	. "github.com/zrepl/zrepl/util"
	"github.com/zrepl/zrepl/zfs"
	"io"
	"reflect"
)

type RPCRequester interface {
	FilesystemRequest(r FilesystemRequest) (roots []zfs.DatasetPath, err error)
	FilesystemVersionsRequest(r FilesystemVersionsRequest) (versions []zfs.FilesystemVersion, err error)
	InitialTransferRequest(r InitialTransferRequest) (io.Reader, error)
	IncrementalTransferRequest(r IncrementalTransferRequest) (io.Reader, error)
}

type RPCHandler interface {
	HandleFilesystemRequest(r FilesystemRequest) (roots []zfs.DatasetPath, err error)

	// returned versions ordered by birthtime, oldest first
	HandleFilesystemVersionsRequest(r FilesystemVersionsRequest) (versions []zfs.FilesystemVersion, err error)

	HandleInitialTransferRequest(r InitialTransferRequest) (io.Reader, error)
	HandleIncrementalTransferRequest(r IncrementalTransferRequest) (io.Reader, error)
}

type Logger interface {
	Printf(format string, args ...interface{})
}

const ByteStreamRPCProtocolVersion = 1

type ByteStreamRPC struct {
	conn    io.ReadWriteCloser
	encoder *json.Encoder
	decoder *json.Decoder
	log     Logger
}

func ConnectByteStreamRPC(conn io.ReadWriteCloser) (RPCRequester, error) {
	// TODO do ssh connection to transport, establish TCP-like communication channel
	rpc := ByteStreamRPC{
		conn:    conn,
		encoder: json.NewEncoder(conn),
		decoder: json.NewDecoder(conn),
	}

	// Assert protocol versions are equal
	err := rpc.ProtocolVersionRequest()
	if err != nil {
		return nil, err
	}

	return rpc, nil
}

func ListenByteStreamRPC(conn io.ReadWriteCloser, handler RPCHandler, log Logger) error {

	// A request consists of two subsequent JSON objects
	// Object 1: RequestHeader => contains type of Request Body
	// Object 2: RequestBody, e.g. IncrementalTransferRequest
	// A response is always a ResponseHeader followed by bytes to be interpreted
	// 	as indicated by the ResponseHeader.ResponseType, e.g.
	// 	 a) a chunked response
	//   b) or another JSON object

	defer conn.Close()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	for {

		var header RequestHeader = RequestHeader{}
		if err := decoder.Decode(&header); err != nil {
			respondWithError(encoder, EDecodeHeader, err)
			return conn.Close()
		}

		switch header.Type {
		case RTProtocolVersionRequest:
			var rq ByteStreamRPCProtocolVersionRequest
			if err := decoder.Decode(&rq); err != nil {
				respondWithError(encoder, EDecodeRequestBody, nil)
				return conn.Close()
			}

			if rq.ClientVersion != ByteStreamRPCProtocolVersion {
				respondWithError(encoder, EProtocolVersionMismatch, nil)
				return conn.Close()
			}

			r := ResponseHeader{
				RequestId:    header.Id,
				ResponseType: ROK,
			}
			if err := encoder.Encode(&r); err != nil {
				panic(err)
			}

		case RTFilesystemRequest:

			var rq FilesystemRequest
			if err := decoder.Decode(&rq); err != nil {
				respondWithError(encoder, EDecodeRequestBody, nil)
				return conn.Close()
			}

			roots, err := handler.HandleFilesystemRequest(rq)
			if err != nil {
				respondWithError(encoder, EHandler, err)
				return conn.Close()
			} else {
				r := ResponseHeader{
					RequestId:    header.Id,
					ResponseType: RFilesystems,
				}
				if err := encoder.Encode(&r); err != nil {
					panic(err)
				}
				if err := encoder.Encode(&roots); err != nil {
					panic(err)
				}
			}

		case RTFilesystemVersionsRequest:

			var rq FilesystemVersionsRequest
			if err := decoder.Decode(&rq); err != nil {
				respondWithError(encoder, EDecodeRequestBody, err)
				return err
			}

			diff, err := handler.HandleFilesystemVersionsRequest(rq)
			if err != nil {
				respondWithError(encoder, EHandler, err)
				return err
			} else {
				r := ResponseHeader{
					RequestId:    header.Id,
					ResponseType: RFilesystemDiff,
				}
				if err := encoder.Encode(&r); err != nil {
					panic(err)
				}
				if err := encoder.Encode(&diff); err != nil {
					panic(err)
				}
			}

		case RTInitialTransferRequest:
			var rq InitialTransferRequest
			if err := decoder.Decode(&rq); err != nil {
				respondWithError(encoder, EDecodeRequestBody, nil)
				return conn.Close()
			}

			snapReader, err := handler.HandleInitialTransferRequest(rq)
			if err != nil {
				respondWithError(encoder, EHandler, err)
				return conn.Close()
			} else {

				r := ResponseHeader{
					RequestId:    header.Id,
					ResponseType: RChunkedStream,
				}
				if err := encoder.Encode(&r); err != nil {
					panic(err)
				}

				chunker := NewChunker(snapReader)
				_, err := io.Copy(conn, &chunker)
				if err != nil {
					panic(err)
				}
			}

		case RTIncrementalTransferRequest:

			var rq IncrementalTransferRequest
			if err := decoder.Decode(&rq); err != nil {
				respondWithError(encoder, EDecodeRequestBody, nil)
				return conn.Close()
			}

			snapReader, err := handler.HandleIncrementalTransferRequest(rq)
			if err != nil {
				respondWithError(encoder, EHandler, err)
			} else {

				r := ResponseHeader{
					RequestId:    header.Id,
					ResponseType: RChunkedStream,
				}
				if err := encoder.Encode(&r); err != nil {
					panic(err)
				}

				chunker := NewChunker(snapReader)
				_, err := io.Copy(conn, &chunker)
				if err != nil {
					panic(err)
				}
			}

		default:
			respondWithError(encoder, EUnknownRequestType, nil)
			return conn.Close()
		}
	}

	return nil
}

func respondWithError(encoder *json.Encoder, id ErrorId, err error) {

	r := ResponseHeader{
		ErrorId:      id,
		ResponseType: RNONE,
		Message:      err.Error(),
	}
	if err := encoder.Encode(&r); err != nil {
		panic(err)
	}

}

func inferRequestType(v interface{}) (RequestType, error) {
	switch v.(type) {
	case ByteStreamRPCProtocolVersionRequest:
		return RTProtocolVersionRequest, nil
	case FilesystemRequest:
		return RTFilesystemRequest, nil
	case FilesystemVersionsRequest:
		return RTFilesystemVersionsRequest, nil
	case InitialTransferRequest:
		return RTInitialTransferRequest, nil
	case IncrementalTransferRequest:
		return RTIncrementalTransferRequest, nil
	default:
		return 0, errors.New(fmt.Sprintf("cannot infer request type for type '%v'",
			reflect.TypeOf(v)))
	}
}

func genUUID() [16]byte {
	return [16]byte{} // TODO
}

func (c ByteStreamRPC) sendRequest(v interface{}) (err error) {

	var rt RequestType

	if rt, err = inferRequestType(v); err != nil {
		return
	}

	h := RequestHeader{
		Type: rt,
		Id:   genUUID(),
	}

	if err = c.encoder.Encode(h); err != nil {
		return
	}
	if err = c.encoder.Encode(v); err != nil {
		return
	}

	return
}

func (c ByteStreamRPC) expectResponseType(rt ResponseType) (err error) {
	var h ResponseHeader
	if err = c.decoder.Decode(&h); err != nil {
		return
	}

	if h.ResponseType != rt {
		return errors.New(fmt.Sprintf("unexpected response type in response header: got %#v, expected %#v. response header: %#v",
			h.ResponseType, rt, h))
	}
	return
}

func (c ByteStreamRPC) sendRequestReceiveHeader(request interface{}, rt ResponseType) (err error) {

	if err = c.sendRequest(request); err != nil {
		return err
	}

	if err = c.expectResponseType(rt); err != nil {
		return err
	}

	return nil
}

func (c ByteStreamRPC) ProtocolVersionRequest() (err error) {
	b := ByteStreamRPCProtocolVersionRequest{
		ClientVersion: ByteStreamRPCProtocolVersion,
	}

	// OK response means the remote side can cope with our protocol version
	return c.sendRequestReceiveHeader(b, ROK)
}

func (c ByteStreamRPC) FilesystemRequest(r FilesystemRequest) (roots []zfs.DatasetPath, err error) {

	if err = c.sendRequestReceiveHeader(r, RFilesystems); err != nil {
		return
	}

	roots = make([]zfs.DatasetPath, 0)

	if err = c.decoder.Decode(&roots); err != nil {
		return
	}

	return
}

func (c ByteStreamRPC) FilesystemVersionsRequest(r FilesystemVersionsRequest) (versions []zfs.FilesystemVersion, err error) {

	if err = c.sendRequestReceiveHeader(r, RFilesystemDiff); err != nil {
		return
	}

	err = c.decoder.Decode(&versions)
	return
}

func (c ByteStreamRPC) InitialTransferRequest(r InitialTransferRequest) (unchunker io.Reader, err error) {

	if err = c.sendRequestReceiveHeader(r, RChunkedStream); err != nil {
		return
	}
	unchunker = NewUnchunker(c.conn)
	return
}

func (c ByteStreamRPC) IncrementalTransferRequest(r IncrementalTransferRequest) (unchunker io.Reader, err error) {
	if err = c.sendRequestReceiveHeader(r, RChunkedStream); err != nil {
		return
	}
	unchunker = NewUnchunker(c.conn)
	return
}

type LocalRPC struct {
	handler RPCHandler
}

func ConnectLocalRPC(handler RPCHandler) RPCRequester {
	return LocalRPC{handler}
}

func (c LocalRPC) FilesystemRequest(r FilesystemRequest) (roots []zfs.DatasetPath, err error) {
	return c.handler.HandleFilesystemRequest(r)
}

func (c LocalRPC) FilesystemVersionsRequest(r FilesystemVersionsRequest) (versions []zfs.FilesystemVersion, err error) {
	return c.handler.HandleFilesystemVersionsRequest(r)
}

func (c LocalRPC) InitialTransferRequest(r InitialTransferRequest) (io.Reader, error) {
	return c.handler.HandleInitialTransferRequest(r)
}

func (c LocalRPC) IncrementalTransferRequest(r IncrementalTransferRequest) (reader io.Reader, err error) {
	reader, err = c.handler.HandleIncrementalTransferRequest(r)
	return
}
