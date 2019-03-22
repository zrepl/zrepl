package rpc

import (
	"context"
	"time"

	"google.golang.org/grpc"

	"github.com/zrepl/zrepl/endpoint"
	"github.com/zrepl/zrepl/replication/logic/pdu"
	"github.com/zrepl/zrepl/rpc/dataconn"
	"github.com/zrepl/zrepl/rpc/grpcclientidentity"
	"github.com/zrepl/zrepl/rpc/netadaptor"
	"github.com/zrepl/zrepl/rpc/versionhandshake"
	"github.com/zrepl/zrepl/transport"
	"github.com/zrepl/zrepl/util/envconst"
)

type Handler interface {
	pdu.ReplicationServer
	dataconn.Handler
}

type serveFunc func(ctx context.Context, demuxedListener transport.AuthenticatedListener, errOut chan<- error)

// Server abstracts the accept and request routing infrastructure for the
// passive side of a replication setup.
type Server struct {
	logger             Logger
	handler            Handler
	controlServer      *grpc.Server
	controlServerServe serveFunc
	dataServer         *dataconn.Server
	dataServerServe    serveFunc
}

type HandlerContextInterceptor func(ctx context.Context) context.Context

// config must be valid (use its Validate function).
func NewServer(handler Handler, loggers Loggers, ctxInterceptor HandlerContextInterceptor) *Server {

	// setup control server
	tcs := grpcclientidentity.NewTransportCredentials(loggers.Control) // TODO different subsystem for log
	unary, stream := grpcclientidentity.NewInterceptors(loggers.Control, endpoint.ClientIdentityKey)
	controlServer := grpc.NewServer(grpc.Creds(tcs), grpc.UnaryInterceptor(unary), grpc.StreamInterceptor(stream))
	pdu.RegisterReplicationServer(controlServer, handler)
	controlServerServe := func(ctx context.Context, controlListener transport.AuthenticatedListener, errOut chan<- error) {
		// give time for graceful stop until deadline expires, then hard stop
		go func() {
			<-ctx.Done()
			if dl, ok := ctx.Deadline(); ok {
				go time.AfterFunc(dl.Sub(dl), controlServer.Stop)
			}
			loggers.Control.Debug("shutting down control server")
			controlServer.GracefulStop()
		}()

		errOut <- controlServer.Serve(netadaptor.New(controlListener, loggers.Control))
	}

	// setup data server
	dataServerClientIdentitySetter := func(ctx context.Context, wire *transport.AuthConn) (context.Context, *transport.AuthConn) {
		ci := wire.ClientIdentity()
		ctx = context.WithValue(ctx, endpoint.ClientIdentityKey, ci)
		if ctxInterceptor != nil {
			ctx = ctxInterceptor(ctx) // SHADOWING
		}
		return ctx, wire
	}
	dataServer := dataconn.NewServer(dataServerClientIdentitySetter, loggers.Data, handler)
	dataServerServe := func(ctx context.Context, dataListener transport.AuthenticatedListener, errOut chan<- error) {
		dataServer.Serve(ctx, dataListener)
		errOut <- nil // TODO bad design of dataServer?
	}

	server := &Server{
		logger:             loggers.General,
		handler:            handler,
		controlServer:      controlServer,
		controlServerServe: controlServerServe,
		dataServer:         dataServer,
		dataServerServe:    dataServerServe,
	}

	return server
}

// The context is used for cancellation only.
// Serve never returns an error, it logs them to the Server's logger.
func (s *Server) Serve(ctx context.Context, l transport.AuthenticatedListener) {
	ctx, cancel := context.WithCancel(ctx)

	l = versionhandshake.Listener(l, envconst.Duration("ZREPL_RPC_SERVER_VERSIONHANDSHAKE_TIMEOUT", 10*time.Second))

	// it is important that demux's context is cancelled,
	// it has background goroutines attached
	demuxListener := demux(ctx, l)

	serveErrors := make(chan error, 2)
	go s.controlServerServe(ctx, demuxListener.control, serveErrors)
	go s.dataServerServe(ctx, demuxListener.data, serveErrors)
	select {
	case serveErr := <-serveErrors:
		s.logger.WithError(serveErr).Error("serve error")
		s.logger.Debug("wait for other server to shut down")
		cancel()
		secondServeErr := <-serveErrors
		s.logger.WithError(secondServeErr).Error("serve error")
	case <-ctx.Done():
		s.logger.Debug("context cancelled, wait for control and data servers")
		cancel()
		for i := 0; i < 2; i++ {
			<-serveErrors
		}
		s.logger.Debug("control and data server shut down, returning from Serve")
	}
}
