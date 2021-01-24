package dataconn

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"

	"google.golang.org/protobuf/proto"

	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/replication/logic/pdu"
	"github.com/zrepl/zrepl/rpc/dataconn/stream"
	"github.com/zrepl/zrepl/transport"
)

// WireInterceptor has a chance to exchange the context and connection on each client connection.
type WireInterceptor func(ctx context.Context, rawConn *transport.AuthConn) (context.Context, *transport.AuthConn)

// Handler implements the functionality that is exposed by Server to the Client.
type Handler interface {
	// Send handles a SendRequest.
	// The returned io.ReadCloser is allowed to be nil, for example if the requested Send is a dry-run.
	Send(ctx context.Context, r *pdu.SendReq) (*pdu.SendRes, io.ReadCloser, error)
	// Receive handles a ReceiveRequest.
	// It is guaranteed that Server calls Receive with a stream that holds the IdleConnTimeout
	// configured in ServerConfig.Shared.IdleConnTimeout.
	Receive(ctx context.Context, r *pdu.ReceiveReq, receive io.ReadCloser) (*pdu.ReceiveRes, error)
	// PingDataconn handles a PingReq
	PingDataconn(ctx context.Context, r *pdu.PingReq) (*pdu.PingRes, error)
}

type Logger = logger.Logger

type ContextInterceptorData interface {
	FullMethod() string
	ClientIdentity() string
}

type ContextInterceptor = func(ctx context.Context, data ContextInterceptorData, handler func(ctx context.Context))

type Server struct {
	h   Handler
	wi  WireInterceptor
	ci  ContextInterceptor
	log Logger
}

var noopContextInteceptor = func(ctx context.Context, _ ContextInterceptorData, handler func(context.Context)) {
	handler(ctx)
}

// wi and ci may be nil
func NewServer(wi WireInterceptor, ci ContextInterceptor, logger Logger, handler Handler) *Server {
	if ci == nil {
		ci = noopContextInteceptor
	}
	return &Server{
		h:   handler,
		wi:  wi,
		ci:  ci,
		log: logger,
	}
}

// Serve consumes the listener, closes it as soon as ctx is closed.
// No accept errors are returned: they are logged to the Logger passed
// to the constructor.
func (s *Server) Serve(ctx context.Context, l transport.AuthenticatedListener) {
	var wg sync.WaitGroup
	defer wg.Wait()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		s.log.Debug("context done, closing listener")
		if err := l.Close(); err != nil {
			s.log.WithError(err).Error("cannot close listener")
		}
	}()
	conns := make(chan *transport.AuthConn)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(conns)
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
		wg.Add(1)
		go func(conn *transport.AuthConn) {
			defer wg.Done()
			s.serveConn(conn)
		}(conn)
	}
}

type contextInterceptorData struct {
	fullMethod     string
	clientIdentity string
}

func (d contextInterceptorData) FullMethod() string     { return d.fullMethod }
func (d contextInterceptorData) ClientIdentity() string { return d.clientIdentity }

func (s *Server) serveConn(nc *transport.AuthConn) {
	s.log.Debug("serveConn begin")
	defer s.log.Debug("serveConn done")

	ctx := context.Background()
	if s.wi != nil {
		ctx, nc = s.wi(ctx, nc)
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
	endpoint := string(header)

	data := contextInterceptorData{
		fullMethod:     endpoint,
		clientIdentity: nc.ClientIdentity(),
	}
	s.ci(ctx, data, func(ctx context.Context) {
		s.serveConnRequest(ctx, endpoint, c)
	})
}

func (s *Server) serveConnRequest(ctx context.Context, endpoint string, c *stream.Conn) {

	reqStructured, err := c.ReadStreamedMessage(ctx, RequestStructuredMaxSize, ReqStructured)
	if err != nil {
		s.log.WithError(err).Error("error reading structured part")
		return
	}

	s.log.WithField("endpoint", endpoint).Debug("calling handler")

	var res proto.Message
	var sendStream io.ReadCloser
	var handlerErr error
	switch endpoint {
	case EndpointSend:
		var req pdu.SendReq
		if err := proto.Unmarshal(reqStructured, &req); err != nil {
			s.log.WithError(err).Error("cannot unmarshal send request")
			return
		}
		res, sendStream, handlerErr = s.h.Send(ctx, &req) // SHADOWING
	case EndpointRecv:
		var req pdu.ReceiveReq
		if err := proto.Unmarshal(reqStructured, &req); err != nil {
			s.log.WithError(err).Error("cannot unmarshal receive request")
			return
		}
		stream, err := c.ReadStream(ZFSStream, false)
		if err != nil {
			s.log.WithError(err).Error("cannot open stream in receive request")
			return
		}
		res, handlerErr = s.h.Receive(ctx, &req, stream) // SHADOWING
	case EndpointPing:
		var req pdu.PingReq
		if err := proto.Unmarshal(reqStructured, &req); err != nil {
			s.log.WithError(err).Error("cannot unmarshal ping request")
			return
		}
		res, handlerErr = s.h.PingDataconn(ctx, &req) // SHADOWING
	default:
		s.log.WithField("endpoint", endpoint).Error("unknown endpoint")
		handlerErr = fmt.Errorf("requested endpoint does not exist")
	}

	s.log.WithField("endpoint", endpoint).WithField("errType", fmt.Sprintf("%T", handlerErr)).Debug("handler returned")

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
		closeErr := sendStream.Close()
		if closeErr != nil {
			s.log.WithError(err).Error("cannot close send stream")
		}
		if err != nil {
			s.log.WithError(err).Error("cannot write send stream")
		}
	}
}
