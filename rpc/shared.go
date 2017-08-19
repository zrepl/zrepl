package rpc

import (
	"fmt"
	"github.com/pkg/errors"
	"io"
	"reflect"
)

type RPCServer interface {
	Serve() (err error)
	RegisterEndpoint(name string, handler interface{}) (err error)
}

type RPCClient interface {
	Call(endpoint string, in, out interface{}) (err error)
	Close() (err error)
}

type Logger interface {
	Printf(format string, args ...interface{})
}

type noLogger struct{}

func (l noLogger) Printf(format string, args ...interface{}) {}
func typeIsIOReader(t reflect.Type) bool {
	return t == reflect.TypeOf((*io.Reader)(nil)).Elem()
}

func typeIsIOReaderPtr(t reflect.Type) bool {
	return t == reflect.TypeOf((*io.Reader)(nil))
}

// An error returned by the Client if the response indicated a status code other than StatusOK
type RPCError struct {
	ResponseHeader *Header
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("%s: %s", e.ResponseHeader.Error, e.ResponseHeader.ErrorMessage)
}

type RPCProtoError struct {
	Message         string
	UnderlyingError error
}

func (e *RPCProtoError) Error() string {
	return e.Message
}

func checkRPCParamTypes(in, out reflect.Type) (err error) {
	if !(in.Kind() == reflect.Ptr || typeIsIOReader(in)) {
		err = errors.Errorf("input parameter must be a pointer or an io.Reader, is of kind %s, type %s", in.Kind(), in)
		return
	}
	if !(out.Kind() == reflect.Ptr) {
		err = errors.Errorf("second input parameter (the non-error output parameter) must be a pointer or an *io.Reader")
		return
	}
	return nil
}

func checkRPCReturnType(rt reflect.Type) (err error) {
	errInterfaceType := reflect.TypeOf((*error)(nil)).Elem()
	if !rt.Implements(errInterfaceType) {
		err = errors.Errorf("handler must return an error")
		return
	}
	return nil
}

func makeEndpointDescr(handler interface{}) (descr endpointDescr, err error) {

	ht := reflect.TypeOf(handler)

	if ht.Kind() != reflect.Func {
		err = errors.Errorf("handler must be of kind reflect.Func")
		return
	}

	if ht.NumIn() != 2 || ht.NumOut() != 1 {
		err = errors.Errorf("handler must have exactly two input parameters and one output parameter")
		return
	}
	if err = checkRPCParamTypes(ht.In(0), ht.In(1)); err != nil {
		return
	}
	if err = checkRPCReturnType(ht.Out(0)); err != nil {
		return
	}

	descr.handler = reflect.ValueOf(handler)
	descr.inType.local = ht.In(0)
	descr.outType.local = ht.In(1)

	if typeIsIOReader(ht.In(0)) {
		descr.inType.proto = DataTypeOctets
	} else {
		descr.inType.proto = DataTypeMarshaledJSON
	}

	if typeIsIOReaderPtr(ht.In(1)) {
		descr.outType.proto = DataTypeOctets
	} else {
		descr.outType.proto = DataTypeMarshaledJSON
	}

	return
}
