// Package grpchelper wraps the adaptors implemented by package grpcclientidentity into a less flexible API
// which, however, ensures that the individual adaptor primitive's expectations are met and hence do not panic.
package grpchelper

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"

	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/rpc/grpcclientidentity"
	"github.com/zrepl/zrepl/rpc/netadaptor"
	"github.com/zrepl/zrepl/tracing"
	"github.com/zrepl/zrepl/transport"
)

// The following constants are relevant for interoperability.
// We use the same values for client & server, because zrepl is more
// symmetrical ("one source, one sink") instead of the typical
// gRPC scenario ("many clients, single server")
const (
	StartKeepalivesAfterInactivityDuration = 5 * time.Second
	KeepalivePeerTimeout                   = 10 * time.Second
)

type Logger = logger.Logger

// ClientConn is an easy-to-use wrapper around the Dialer and TransportCredentials interface
// to produce a grpc.ClientConn
func ClientConn(cn transport.Connecter, log Logger, genReqID func() string) *grpc.ClientConn {
	ka := grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:                StartKeepalivesAfterInactivityDuration,
		Timeout:             KeepalivePeerTimeout,
		PermitWithoutStream: true,
	})
	unaryIntcpt := grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {

		traceStack := tracing.GetStack(ctx)
		if len(traceStack) == 0 {
			panic("implementation error: expecting trace stack")
		}
		reqId := genReqID() + " " + strings.Join(traceStack, " <> ")

		l := log
		l = l.WithField("reqID", reqId)
		l = l.WithField("reqT", fmt.Sprintf("%T", req))
		l = l.WithField("repT", fmt.Sprintf("%T", reply))
		l = l.WithField("ctxT", fmt.Sprintf("%T", ctx))
		l.Debug("unary intercepted")

		ctx = metadata.AppendToOutgoingContext(ctx, "zrepl-req-id", reqId)

		err := invoker(ctx, method, req, reply, cc, opts...)

		l = log.WithField("repT", fmt.Sprintf("%T", reply))
		l = log.WithField("errT", fmt.Sprintf("%T", err))
		l.Debug("reply intercepted")

		return err
	})
	dialerOption := grpc.WithDialer(grpcclientidentity.NewDialer(log, cn))
	cred := grpc.WithTransportCredentials(grpcclientidentity.NewTransportCredentials(log))
	cc, err := grpc.DialContext(context.Background(), "doesn't matter done by dialer", dialerOption, cred, ka, unaryIntcpt)
	if err != nil {
		log.WithError(err).Error("cannot create gRPC client conn (non-blocking)")
		// It's ok to panic here: the we call grpc.DialContext without the
		// (grpc.WithBlock) dial option, and at the time of writing, the grpc
		// docs state that no connection attempt is made in that case.
		// Hence, any error that occurs is due to DialOptions or similar,
		// and thus indicative of an implementation error.
		panic(err)
	}
	return cc
}

// NewServer is a convenience interface around the TransportCredentials and Interceptors interface.
func NewServer(authListener transport.AuthenticatedListener, clientIdentityKey interface{}, logger grpcclientidentity.Logger, interceptor grpcclientidentity.ContextInterceptor, pre grpcclientidentity.PreHandlerInspector, post grpcclientidentity.PostHandlerInspector) (srv *grpc.Server, serve func() error) {
	ka := grpc.KeepaliveParams(keepalive.ServerParameters{
		Time:    StartKeepalivesAfterInactivityDuration,
		Timeout: KeepalivePeerTimeout,
	})
	ep := grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
		MinTime:             StartKeepalivesAfterInactivityDuration / 2, // avoid skew
		PermitWithoutStream: true,
	})
	tcs := grpcclientidentity.NewTransportCredentials(logger)
	unary, stream := grpcclientidentity.NewInterceptors(logger, clientIdentityKey, interceptor, pre, post)
	unary2 := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			logger.Error("no request id in incoming request")
		} else {
			logger.Info(fmt.Sprintf("req-id = %v", md.Get("zrepl-req-id")))
		}
		return unary(ctx, req, info, handler)
	}
	srv = grpc.NewServer(grpc.Creds(tcs), grpc.UnaryInterceptor(unary2), grpc.StreamInterceptor(stream), ka, ep)

	serve = func() error {
		if err := srv.Serve(netadaptor.New(authListener, logger)); err != nil {
			return err
		}
		return nil
	}

	return srv, serve
}
