package rpc

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"

	"github.com/pkg/errors"
)

type Server struct {
	ml        *MessageLayer
	logger    Logger
	endpoints map[string]endpointDescr
}

type typeMap struct {
	local reflect.Type
	proto DataType
}
type endpointDescr struct {
	inType  typeMap
	outType typeMap
	handler reflect.Value
}

type MarshaledJSONEndpoint func(bodyJSON interface{})

func NewServer(rwc io.ReadWriteCloser) *Server {
	ml := NewMessageLayer(rwc)
	return &Server{
		ml, noLogger{}, make(map[string]endpointDescr),
	}
}

func (s *Server) SetLogger(logger Logger, logMessageLayer bool) {
	s.logger = logger
	if logMessageLayer {
		s.ml.logger = logger
	} else {
		s.ml.logger = noLogger{}
	}
}

func (s *Server) RegisterEndpoint(name string, handler interface{}) (err error) {
	_, ok := s.endpoints[name]
	if ok {
		return errors.Errorf("already set up an endpoint for '%s'", name)
	}
	s.endpoints[name], err = makeEndpointDescr(handler)
	return
}

func checkResponseHeader(h *Header) (err error) {
	var statusNotSet Status
	if h.Error == statusNotSet {
		return errors.Errorf("status has zero-value")
	}
	return nil
}

func (s *Server) writeResponse(h *Header) (err error) {
	// TODO validate
	return s.ml.WriteHeader(h)
}

func (s *Server) recvRequest() (h *Header, err error) {
	h, err = s.ml.ReadHeader()
	if err != nil {
		s.logger.Printf("error reading header: %s", err)
		return nil, err
	}

	s.logger.Printf("validating request")
	err = nil // TODO validate
	if err == nil {
		return h, nil
	}
	s.logger.Printf("request validation error: %s", err)

	r := NewErrorHeader(StatusRequestError, "%s", err)
	return nil, s.writeResponse(r)
}

var doneServeNext error = errors.New("this should not cause a HangUp() in the server")

var ProtocolError error = errors.New("protocol error, server should hang up")

// Serve the connection until failure or the client hangs up
func (s *Server) Serve() (err error) {
	for {

		err = s.ServeRequest()

		if err == nil {
			continue
		}

		if err == doneServeNext {
			s.logger.Printf("subroutine returned pseudo-error indicating early-exit")
			continue
		}

		s.logger.Printf("hanging up after ServeRequest returned error: %s", err)
		s.ml.HangUp()
		return err
	}
}

// Serve a single request
// * wait for request to come in
// * call handler
// * reply
//
// The connection is left open, the next bytes on the conn should be
// the next request header.
//
// Returns an err != nil if the error is bad enough to hang up on the client.
// Examples: 		protocol version mismatches, protocol errors in general, ...
// Non-Examples:	a handler error
func (s *Server) ServeRequest() (err error) {

	ml := s.ml

	s.logger.Printf("reading header")
	h, err := s.recvRequest()
	if err != nil {
		return err
	}

	ep, ok := s.endpoints[h.Endpoint]
	if !ok {
		r := NewErrorHeader(StatusRequestError, "unregistered endpoint %s", h.Endpoint)
		return s.writeResponse(r)
	}

	if ep.inType.proto != h.DataType {
		r := NewErrorHeader(StatusRequestError, "wrong DataType for endpoint %s (has %s, you provided %s)", h.Endpoint, ep.inType.proto, h.DataType)
		return s.writeResponse(r)
	}

	if ep.outType.proto != h.Accept {
		r := NewErrorHeader(StatusRequestError, "wrong Accept for endpoint %s (has %s, you provided %s)", h.Endpoint, ep.outType.proto, h.Accept)
		return s.writeResponse(r)
	}

	dr := ml.ReadData()

	// Determine inval
	var inval reflect.Value
	switch ep.inType.proto {
	case DataTypeMarshaledJSON:
		// Unmarshal input
		inval = reflect.New(ep.inType.local.Elem())
		invalIface := inval.Interface()
		err = json.NewDecoder(dr).Decode(invalIface)
		if err != nil {
			r := NewErrorHeader(StatusRequestError, "cannot decode marshaled JSON: %s", err)
			return s.writeResponse(r)
		}
	case DataTypeOctets:
		// Take data as is
		inval = reflect.ValueOf(dr)
	default:
		panic("not implemented")
	}

	outval := reflect.New(ep.outType.local.Elem()) // outval is a double pointer

	s.logger.Printf("before handler, inval=%v outval=%v", inval, outval)

	// Call the handler
	errs := ep.handler.Call([]reflect.Value{inval, outval})

	if !errs[0].IsNil() {
		he := errs[0].Interface().(error) // we checked that before...
		s.logger.Printf("handler returned error: %s", err)
		r := NewErrorHeader(StatusError, "%s", he.Error())
		return s.writeResponse(r)
	}

	switch ep.outType.proto {

	case DataTypeMarshaledJSON:

		var dataBuf bytes.Buffer
		// Marshal output
		err = json.NewEncoder(&dataBuf).Encode(outval.Interface())
		if err != nil {
			r := NewErrorHeader(StatusServerError, "cannot marshal response: %s", err)
			return s.writeResponse(r)
		}

		replyHeader := Header{
			Error:    StatusOK,
			DataType: ep.outType.proto,
		}
		if err = s.writeResponse(&replyHeader); err != nil {
			return err
		}

		if err = ml.WriteData(&dataBuf); err != nil {
			return
		}

	case DataTypeOctets:

		h := Header{
			Error:    StatusOK,
			DataType: DataTypeOctets,
		}
		if err = s.writeResponse(&h); err != nil {
			return
		}

		reader := outval.Interface().(*io.Reader) // we checked that when adding the endpoint
		err = ml.WriteData(*reader)
		if err != nil {
			return err
		}

	}

	return nil
}
