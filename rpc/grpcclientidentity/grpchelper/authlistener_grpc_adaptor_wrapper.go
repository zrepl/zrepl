// Package grpchelper wraps the adaptors implemented by package grpcclientidentity into a less flexible API
// which, however, ensures that the individual adaptor primitive's expectations are met and hence do not panic.
package grpchelper

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/rpc/grpcclientidentity"
	"github.com/zrepl/zrepl/rpc/netadaptor"
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
func ClientConn(cn transport.Connecter, log Logger) *grpc.ClientConn {
	ka := grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:                StartKeepalivesAfterInactivityDuration,
		Timeout:             KeepalivePeerTimeout,
		PermitWithoutStream: true,
	})
	dialerOption := grpc.WithContextDialer(grpcclientidentity.NewDialer(log, cn))
	cred := grpc.WithTransportCredentials(grpcclientidentity.NewTransportCredentials(log))
	// we use context.Background without a timeout here because we don't set grpc.WithBlock
	// => docs:  "In the non-blocking case, the ctx does not act against the connection. It only controls the setup steps."
	cc, err := grpc.DialContext(context.Background(), "doesn't matter done by dialer", dialerOption, cred, ka)
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
func NewServer(authListener transport.AuthenticatedListener, clientIdentityKey interface{}, logger grpcclientidentity.Logger, ctxInterceptor grpcclientidentity.Interceptor) (srv *grpc.Server, serve func() error) {
	ka := grpc.KeepaliveParams(keepalive.ServerParameters{
		Time:    StartKeepalivesAfterInactivityDuration,
		Timeout: KeepalivePeerTimeout,
	})
	ep := grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
		MinTime:             StartKeepalivesAfterInactivityDuration / 2, // avoid skew
		PermitWithoutStream: true,
	})
	tcs := grpcclientidentity.NewTransportCredentials(logger)
	unary, stream := grpcclientidentity.NewInterceptors(logger, clientIdentityKey, ctxInterceptor)
	srv = grpc.NewServer(grpc.Creds(tcs), grpc.UnaryInterceptor(unary), grpc.StreamInterceptor(stream), ka, ep)

	serve = func() error {
		if err := srv.Serve(netadaptor.New(authListener, logger)); err != nil {
			return err
		}
		return nil
	}

	return srv, serve
}
