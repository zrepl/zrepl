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
	dialerOption := grpc.WithDialer(grpcclientidentity.NewDialer(log, cn))
	cred := grpc.WithTransportCredentials(grpcclientidentity.NewTransportCredentials(log))
	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Second)
	defer cancel()
	cc, err := grpc.DialContext(ctx, "doesn't matter done by dialer", dialerOption, cred, ka)
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
func NewServer(authListenerFactory transport.AuthenticatedListenerFactory, clientIdentityKey interface{}, logger grpcclientidentity.Logger) (srv *grpc.Server, serve func() error, err error) {
	ka := grpc.KeepaliveParams(keepalive.ServerParameters{
		Time:    StartKeepalivesAfterInactivityDuration,
		Timeout: KeepalivePeerTimeout,
	})
	tcs := grpcclientidentity.NewTransportCredentials(logger)
	unary, stream := grpcclientidentity.NewInterceptors(logger, clientIdentityKey)
	srv = grpc.NewServer(grpc.Creds(tcs), grpc.UnaryInterceptor(unary), grpc.StreamInterceptor(stream), ka)

	serve = func() error {
		l, err := authListenerFactory()
		if err != nil {
			return err
		}
		if err := srv.Serve(netadaptor.New(l, logger)); err != nil {
			return err
		}
		return nil
	}

	return srv, serve, nil
}
