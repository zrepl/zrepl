package rpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	. "github.com/zrepl/zrepl/util"
	"github.com/zrepl/zrepl/zfs"
	"io"
	"os"
	"reflect"
)

type RPCRequester interface {
	FilesystemRequest(r FilesystemRequest) (roots []zfs.DatasetPath, err error)
	FilesystemVersionsRequest(r FilesystemVersionsRequest) (versions []zfs.FilesystemVersion, err error)
	InitialTransferRequest(r InitialTransferRequest) (io.Reader, error)
	IncrementalTransferRequest(r IncrementalTransferRequest) (io.Reader, error)
	CloseRequest(r CloseRequest) (err error)
	ForceClose() (err error)
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

type ByteStream interface {
	io.ReadWriteCloser
}

type ByteStreamRPC struct {
	conn ByteStream
	log  Logger
}

func ConnectByteStreamRPC(conn ByteStream) (RPCRequester, error) {

	rpc := ByteStreamRPC{
		conn: conn,
	}

	// Assert protocol versions are equal
	err := rpc.ProtocolVersionRequest()
	if err != nil {
		return nil, err
	}

	return rpc, nil
}

type ByteStreamRPCDecodeJSONError struct {
	Type       reflect.Type
	DecoderErr error
}

func (e ByteStreamRPCDecodeJSONError) Error() string {
	return fmt.Sprintf("cannot decode %s: %s", e.Type, e.DecoderErr)
}

func ListenByteStreamRPC(conn ByteStream, handler RPCHandler, log Logger) error {

	// A request consists of two subsequent chunked JSON objects
	// Object 1: RequestHeader => contains type of Request Body
	// Object 2: RequestBody, e.g. IncrementalTransferRequest
	// A response is always a ResponseHeader followed by bytes to be interpreted
	// 	as indicated by the ResponseHeader.ResponseType, e.g.
	// 	 a) a chunked response
	//   b) or another JSON object

	defer conn.Close()

	send := func(r interface{}) {
		if err := writeChunkedJSON(conn, r); err != nil {
			panic(err)
		}
	}

	sendError := func(id ErrorId, msg string) {
		r := ResponseHeader{
			ErrorId:      id,
			ResponseType: RNONE,
			Message:      msg,
		}
		log.Printf("sending error response: %#v", r)
		if err := writeChunkedJSON(conn, r); err != nil {
			log.Printf("error sending error response: %#v", err)
			panic(err)
		}
	}

	recv := func(r interface{}) (err error) {
		return readChunkedJSON(conn, r)
	}

	for {

		var header RequestHeader = RequestHeader{}
		if err := recv(&header); err != nil {
			sendError(EDecodeHeader, err.Error())
			return conn.Close()
		}

		switch header.Type {
		case RTProtocolVersionRequest:
			var rq ByteStreamRPCProtocolVersionRequest
			if err := recv(&rq); err != nil {
				sendError(EDecodeRequestBody, err.Error())
				return conn.Close()
			}

			if rq.ClientVersion != ByteStreamRPCProtocolVersion {
				sendError(EProtocolVersionMismatch, "")
				return conn.Close()
			}

			r := ResponseHeader{
				RequestId:    header.Id,
				ResponseType: ROK,
			}
			send(&r)

		case RTCloseRequest:

			var rq CloseRequest
			if err := recv(&rq); err != nil {
				sendError(EDecodeRequestBody, err.Error())
				return conn.Close()
			}

			if rq.Goodbye != "" {
				log.Printf("close request with goodbye: %s", rq.Goodbye)
			}

			send(&ResponseHeader{
				RequestId:    header.Id,
				ResponseType: ROK,
			})

			return conn.Close()

		case RTFilesystemRequest:

			var rq FilesystemRequest
			if err := recv(&rq); err != nil {
				sendError(EDecodeRequestBody, "")
				return conn.Close()
			}

			roots, err := handler.HandleFilesystemRequest(rq)
			if err != nil {
				sendError(EHandler, err.Error())
				return conn.Close()
			} else {
				r := ResponseHeader{
					RequestId:    header.Id,
					ResponseType: RFilesystems,
				}
				send(&r)
				send(&roots)
			}

		case RTFilesystemVersionsRequest:

			var rq FilesystemVersionsRequest
			if err := recv(&rq); err != nil {
				sendError(EDecodeRequestBody, err.Error())
				return err
			}

			diff, err := handler.HandleFilesystemVersionsRequest(rq)
			if err != nil {
				sendError(EHandler, err.Error())
				return err
			} else {
				r := ResponseHeader{
					RequestId:    header.Id,
					ResponseType: RFilesystemDiff,
				}
				send(&r)
				send(&diff)
			}

		case RTInitialTransferRequest:
			var rq InitialTransferRequest
			if err := recv(&rq); err != nil {
				sendError(EDecodeRequestBody, "")
				return conn.Close()
			}
			log.Printf("initial transfer request: %#v", rq)

			snapReader, err := handler.HandleInitialTransferRequest(rq)
			if err != nil {
				sendError(EHandler, err.Error())
				return conn.Close()
			} else {

				r := ResponseHeader{
					RequestId:    header.Id,
					ResponseType: RChunkedStream,
				}
				send(&r)

				chunker := NewChunker(snapReader)
				_, err := io.Copy(conn, &chunker)
				if err != nil {
					panic(err)
				}
			}

		case RTIncrementalTransferRequest:

			var rq IncrementalTransferRequest
			if err := recv(&rq); err != nil {
				sendError(EDecodeRequestBody, "")
				return conn.Close()
			}

			snapReader, err := handler.HandleIncrementalTransferRequest(rq)
			if err != nil {
				sendError(EHandler, err.Error())
			} else {

				r := ResponseHeader{
					RequestId:    header.Id,
					ResponseType: RChunkedStream,
				}
				send(&r)

				chunker := NewChunker(snapReader)
				_, err := io.Copy(conn, &chunker)
				if err != nil {
					panic(err)
				}
			}

		default:
			sendError(EUnknownRequestType, "")
			return conn.Close()
		}
	}

	return nil
}

func writeChunkedJSON(conn io.Writer, r interface{}) (err error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.Encode(r)
	ch := NewChunker(&buf)
	_, err = io.Copy(conn, &ch)
	return
}

func readChunkedJSON(conn io.ReadWriter, r interface{}) (err error) {
	unch := NewUnchunker(conn)
	dec := json.NewDecoder(unch)
	err = dec.Decode(r)
	if err != nil {
		err = ByteStreamRPCDecodeJSONError{
			Type:       reflect.TypeOf(r),
			DecoderErr: err,
		}
	}
	closeErr := unch.Close()
	if err == nil && closeErr != nil {
		err = closeErr
	}
	return
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
	case CloseRequest:
		return RTCloseRequest, nil
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

	if err = writeChunkedJSON(c.conn, h); err != nil {
		return
	}
	if err = writeChunkedJSON(c.conn, v); err != nil {
		return
	}

	return
}

func (c ByteStreamRPC) expectResponseType(rt ResponseType) (err error) {

	var h ResponseHeader
	if err = readChunkedJSON(c.conn, &h); err != nil {
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

	if err = readChunkedJSON(c.conn, &roots); err != nil {
		return
	}

	return
}

func (c ByteStreamRPC) FilesystemVersionsRequest(r FilesystemVersionsRequest) (versions []zfs.FilesystemVersion, err error) {

	if err = c.sendRequestReceiveHeader(r, RFilesystemDiff); err != nil {
		return
	}

	err = readChunkedJSON(c.conn, &versions)
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

func (c ByteStreamRPC) CloseRequest(r CloseRequest) (err error) {
	if err = c.sendRequestReceiveHeader(r, ROK); err != nil {
		return
	}
	os.Stderr.WriteString("close request conn.Close()")
	err = c.conn.Close()
	return
}

func (c ByteStreamRPC) ForceClose() (err error) {
	return c.conn.Close()
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

func (c LocalRPC) CloseRequest(r CloseRequest) error { return nil }

func (c LocalRPC) ForceClose() error { return nil }
