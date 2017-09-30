package rpc

import (
	"github.com/pkg/errors"
	"reflect"
)

type LocalRPC struct {
	endpoints map[string]reflect.Value
}

func NewLocalRPC() *LocalRPC {
	return &LocalRPC{make(map[string]reflect.Value, 0)}
}

func (s *LocalRPC) RegisterEndpoint(name string, handler interface{}) (err error) {
	_, ok := s.endpoints[name]
	if ok {
		return errors.Errorf("already set up an endpoint for '%s'", name)
	}
	ep, err := makeEndpointDescr(handler)
	if err != nil {
		return err
	}
	s.endpoints[name] = ep.handler
	return nil
}

func (s *LocalRPC) Serve() (err error) {
	panic("local cannot serve")
}

func (c *LocalRPC) Call(endpoint string, in, out interface{}) (err error) {
	ep, ok := c.endpoints[endpoint]
	if !ok {
		panic("implementation error: implementation should not call local RPC without knowing which endpoints exist")
	}

	args := []reflect.Value{reflect.ValueOf(in), reflect.ValueOf(out)}

	if err = checkRPCParamTypes(args[0].Type(), args[1].Type()); err != nil {
		return
	}

	rets := ep.Call(args)

	if len(rets) != 1 {
		panic("implementation error: endpoints must have one error ")
	}
	if err = checkRPCReturnType(rets[0].Type()); err != nil {
		panic(err)
	}

	err = nil
	if !rets[0].IsNil() {
		err = rets[0].Interface().(error) // we checked that above
	}
	return
}

func (c *LocalRPC) Close() (err error) {
	return nil
}
