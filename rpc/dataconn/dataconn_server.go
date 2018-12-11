package dataconn

import (
	"bytes"
	"context"
	"fmt"

	"github.com/golang/protobuf/proto"
	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/replication/pdu"
	"github.com/zrepl/zrepl/rpc/dataconn/stream"
	"github.com/zrepl/zrepl/transport"
	"github.com/zrepl/zrepl/zfs"
)

// WireInterceptor has a chance to exchange the context and connection on each client connection.
type WireInterceptor func(ctx context.Context, rawConn *transport.AuthConn) (context.Context, *transport.AuthConn)

// Handler implements the functionality that is exposed by Server to the Client.
type Handler interface {
	// Send handles a SendRequest.
	// The returned io.ReadCloser is allowed to be nil, for example if the requested Send is a dry-run.
	Send(ctx context.Context, r *pdu.SendReq) (*pdu.SendRes, zfs.StreamCopier, error)
	// Receive handles a ReceiveRequest.
	// It is guaranteed that Server calls Receive with a stream that holds the IdleConnTimeout
	// configured in ServerConfig.Shared.IdleConnTimeout.
	Receive(ctx context.Context, r *pdu.ReceiveReq, receive zfs.StreamCopier) (*pdu.ReceiveRes, error)
}

type Logger = logger.Logger

type Server struct {
	h   Handler
	wi  WireInterceptor
	log Logger
}

func NewServer(wi WireInterceptor, logger Logger, handler Handler) *Server {
	return &Server{
		h:   handler,
		wi:  wi,
		log: logger,
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
		res, sendStream, handlerErr = s.h.Send(ctx, &req) // SHADOWING
	case EndpointRecv:
		var req pdu.ReceiveReq
		if err := proto.Unmarshal(reqStructured, &req); err != nil {
			s.log.WithError(err).Error("cannot unmarshal receive request")
			return
		}
		res, handlerErr = s.h.Receive(ctx, &req, &streamCopier{streamConn: c, closeStreamOnClose: false}) // SHADOWING
	default:
		s.log.WithField("endpoint", endpoint).Error("unknown endpoint")
		handlerErr = fmt.Errorf("requested endpoint does not exist")
		return
	}

	s.log.WithField("endpoint", endpoint).WithField("errType", fmt.Sprintf("%T", handlerErr)).Debug("handler returned")

	// prepare protobuf now to return the protobuf error in the header
	// if marshaling fails. We consider failed marshaling a handler error
	var protobuf *bytes.Buffer
	if handlerErr == nil {
		protobufBytes, err := proto.Marshal(res)
		if err != nil {
			s.log.WithError(err).Error("cannot marshal handler protobuf")
			handlerErr = err
		}
		protobuf = bytes.NewBuffer(protobufBytes) // SHADOWING
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

	return
}
