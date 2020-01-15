package dataconn

import (
	"bytes"
	"context"
	"fmt"

	"github.com/golang/protobuf/proto"

	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/replication/logic/pdu"
	"github.com/zrepl/zrepl/rpc/dataconn/stream"
	"github.com/zrepl/zrepl/transport"
	"github.com/zrepl/zrepl/zfs"
)

// ReqInterceptor has a chance to exchange the context and connection on each request.
type ReqInterceptor func(ctx context.Context, rawConn *transport.AuthConn) (context.Context, *transport.AuthConn)

// Handler implements the functionality that is exposed by Server to the Client.
type Handler interface {
	// Send handles a SendRequest.
	// The returned io.ReadCloser is allowed to be nil, for example if the requested Send is a dry-run.
	Send(ctx context.Context, r *pdu.SendReq) (*pdu.SendRes, zfs.StreamCopier, error)
	// Receive handles a ReceiveRequest.
	// It is guaranteed that Server calls Receive with a stream that holds the IdleConnTimeout
	// configured in ServerConfig.Shared.IdleConnTimeout.
	Receive(ctx context.Context, r *pdu.ReceiveReq, receive zfs.StreamCopier) (*pdu.ReceiveRes, error)
	// PingDataconn handles a PingReq
	PingDataconn(ctx context.Context, r *pdu.PingReq) (*pdu.PingRes, error)
}

type PreHandlerInspector = func(ctx context.Context, endpoint string, req interface{})

type PostHandlerInspector = func(ctx context.Context, response interface{}, err error)

type Logger = logger.Logger

type Server struct {
	h    Handler
	ri   ReqInterceptor
	log  Logger
	pre  PreHandlerInspector
	post PostHandlerInspector
}

func NewServer(ri ReqInterceptor, logger Logger, pre PreHandlerInspector, handler Handler, post PostHandlerInspector) *Server {
	return &Server{
		h:   handler,
		ri:  ri,
		log: logger,
		pre: pre,
		post: post,
	}
}

// Serve consumes the listener, closes it as soon as ctx is closed.
// No accept errors are returned: they are logged to the Logger passed
// to the constructor.
func (s *Server) Serve(ctx context.Context, l transport.AuthenticatedListener) {

	go func() {
		<-ctx.Done()
		s.log.Debug("context done")
		if err := l.Close(); err != nil {
			s.log.WithError(err).Error("cannot close listener")
		}
	}()
	conns := make(chan *transport.AuthConn)
	go func() {
		for {
			conn, err := l.Accept(ctx)
			if err != nil {
				if ctx.Done() != nil {
					s.log.Debug("stop accepting after context is done")
					return
				}
				s.log.WithError(err).Error("accept error")
				continue
			}
			conns <- conn
		}
	}()
	for conn := range conns {
		go s.serveConn(conn)
	}
}

func (s *Server) serveConn(nc *transport.AuthConn) {
	s.log.Debug("serveConn begin")
	defer s.log.Debug("serveConn done")

	ctx := context.Background()
	if s.ri != nil {
		ctx, nc = s.ri(ctx, nc)
	}

	c := stream.Wrap(nc, HeartbeatInterval, HeartbeatPeerTimeout)
	defer func() {
		s.log.Debug("close client connection")
		if err := c.Close(); err != nil {
			s.log.WithError(err).Error("cannot close client connection")
		}
	}()

	header, err := c.ReadStreamedMessage(ctx, RequestHeaderMaxSize, ReqHeader)
	if err != nil {
		s.log.WithError(err).Error("error reading structured part")
		return
	}
	endpoint := string(header) // FIXME

	reqStructured, err := c.ReadStreamedMessage(ctx, RequestStructuredMaxSize, ReqStructured)
	if err != nil {
		s.log.WithError(err).Error("error reading structured part")
		return
	}

	s.log.WithField("endpoint", endpoint).Debug("calling handler")

	var res proto.Message
	var sendStream zfs.StreamCopier
	var handlerErr error
	switch endpoint {
	case EndpointSend:
		var req pdu.SendReq
		if err := proto.Unmarshal(reqStructured, &req); err != nil {
			s.log.WithError(err).Error("cannot unmarshal send request")
			return
		}
		s.pre(ctx, endpoint, &req)
		res, sendStream, handlerErr = s.h.Send(ctx, &req) // SHADOWING
	case EndpointRecv:
		var req pdu.ReceiveReq
		if err := proto.Unmarshal(reqStructured, &req); err != nil {
			s.log.WithError(err).Error("cannot unmarshal receive request")
			return
		}
		s.pre(ctx, endpoint, &req)
		res, handlerErr = s.h.Receive(ctx, &req, &streamCopier{streamConn: c, closeStreamOnClose: false}) // SHADOWING
	case EndpointPing:
		var req pdu.PingReq
		if err := proto.Unmarshal(reqStructured, &req); err != nil {
			s.log.WithError(err).Error("cannot unmarshal ping request")
			return
		}
		s.pre(ctx, endpoint, &req)
		res, handlerErr = s.h.PingDataconn(ctx, &req) // SHADOWING
	default:
		s.log.WithField("endpoint", endpoint).Error("unknown endpoint")
		handlerErr = fmt.Errorf("requested endpoint does not exist")
	}

	s.log.WithField("endpoint", endpoint).WithField("errType", fmt.Sprintf("%T", handlerErr)).Debug("handler returned")

	s.post(ctx, res, handlerErr)

	// prepare protobuf now to return the protobuf error in the header
	// if marshaling fails. We consider failed marshaling a handler error
	var protobuf *bytes.Buffer
	if handlerErr == nil {
		if res == nil {
			handlerErr = fmt.Errorf("implementation error: handler for endpoint %q returns nil error and nil result", endpoint)
			s.log.WithError(err).Error("handle implementation error")
		} else {
			protobufBytes, err := proto.Marshal(res)
			if err != nil {
				s.log.WithError(err).Error("cannot marshal handler protobuf")
				handlerErr = err
			}
			protobuf = bytes.NewBuffer(protobufBytes) // SHADOWING
		}
	}

	var resHeaderBuf bytes.Buffer
	if handlerErr == nil {
		resHeaderBuf.WriteString(responseHeaderHandlerOk)
	} else {
		resHeaderBuf.WriteString(responseHeaderHandlerErrorPrefix)
		resHeaderBuf.WriteString(handlerErr.Error())
	}
	if err := c.WriteStreamedMessage(ctx, &resHeaderBuf, ResHeader); err != nil {
		s.log.WithError(err).Error("cannot write response header")
		return
	}

	if handlerErr != nil {
		s.log.Debug("early exit after handler error")
		return
	}

	if err := c.WriteStreamedMessage(ctx, protobuf, ResStructured); err != nil {
		s.log.WithError(err).Error("cannot write structured part of response")
		return
	}

	if sendStream != nil {
		err := c.SendStream(ctx, sendStream, ZFSStream)
		if err != nil {
			s.log.WithError(err).Error("cannot write send stream")
		}
	}
}
