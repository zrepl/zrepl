// Package grpcclientidentity makes the client identity
// provided by github.com/zrepl/zrepl/daemon/transport/serve.{AuthenticatedListener,AuthConn}
// available to gRPC service handlers.
//
// This goal is achieved through the combination of custom gRPC transport credentials and two interceptors
// (i.e. middleware).
//
// For gRPC clients, the TransportCredentials + Dialer can be used to construct a gRPC client (grpc.ClientConn)
// that uses a  github.com/zrepl/zrepl/daemon/transport/connect.Connecter to connect to a server.
//
// The adaptors exposed by this package must be used together, and panic if they are not.
// See package grpchelper for a more restrictive but safe example on how the adaptors should be composed.
package grpcclientidentity

import (
	"context"
	"fmt"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"

	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/transport"
)

type Logger = logger.Logger

type GRPCDialFunction = func(string, time.Duration) (net.Conn, error)

func NewDialer(logger Logger, connecter transport.Connecter) GRPCDialFunction {
	return func(s string, duration time.Duration) (conn net.Conn, e error) {
		ctx, cancel := context.WithTimeout(context.Background(), duration)
		defer cancel()
		nc, err := connecter.Connect(ctx)
		// TODO find better place (callback from gRPC?) where to log errors
		// we want the users to know, though
		if err != nil {
			logger.WithError(err).Error("cannot connect")
		}
		return nc, err
	}
}

type authConnAuthType struct {
	clientIdentity string
}

func (authConnAuthType) AuthType() string {
	return "AuthConn"
}

type connecterAuthType struct{}

func (connecterAuthType) AuthType() string {
	return "connecter"
}

type transportCredentials struct {
	logger Logger
}

// Use on both sides as ServerOption or ClientOption.
func NewTransportCredentials(log Logger) credentials.TransportCredentials {
	if log == nil {
		log = logger.NewNullLogger()
	}
	return &transportCredentials{log}
}

func (c *transportCredentials) ClientHandshake(ctx context.Context, s string, rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	c.logger.WithField("url", s).WithField("connType", fmt.Sprintf("%T", rawConn)).Debug("ClientHandshake")
	// do nothing, client credential is only for WithInsecure warning to go away
	// the authentication is done by the connecter
	return rawConn, &connecterAuthType{}, nil
}

func (c *transportCredentials) ServerHandshake(rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	c.logger.WithField("connType", fmt.Sprintf("%T", rawConn)).Debug("ServerHandshake")
	authConn, ok := rawConn.(*transport.AuthConn)
	if !ok {
		panic(fmt.Sprintf("NewTransportCredentials must be used with a listener that returns *transport.AuthConn, got %T", rawConn))
	}
	return rawConn, &authConnAuthType{authConn.ClientIdentity()}, nil
}

func (*transportCredentials) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{} // TODO
}

func (t *transportCredentials) Clone() credentials.TransportCredentials {
	var x = *t
	return &x
}

func (*transportCredentials) OverrideServerName(string) error {
	panic("not implemented")
}

type ContextInterceptor = func(ctx context.Context) context.Context

type PreHandlerInspector func(ctx context.Context, endpoint string, req interface{})

type PostHandlerInspector func(ctx context.Context, response interface{}, err error)

func NewInterceptors(logger Logger, clientIdentityKey interface{}, ctxInterceptor ContextInterceptor, pre PreHandlerInspector, post PostHandlerInspector) (unary grpc.UnaryServerInterceptor, stream grpc.StreamServerInterceptor) {
	unary = func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		logger.WithField("fullMethod", info.FullMethod).Debug("request")
		p, ok := peer.FromContext(ctx)
		if !ok {
			panic("peer.FromContext expected to return a peer in grpc.UnaryServerInterceptor")
		}
		logger.WithField("peer_addr", p.Addr.String()).Debug("peer addr")
		a, ok := p.AuthInfo.(*authConnAuthType)
		if !ok {
			panic(fmt.Sprintf("NewInterceptors must be used in combination with grpc.NewTransportCredentials, but got auth type %T", p.AuthInfo))
		}
		logger.WithField("peer_client_identity", a.clientIdentity).Debug("peer client identity")
		ctx = context.WithValue(ctx, clientIdentityKey, a.clientIdentity)
		if ctxInterceptor != nil {
			ctx = ctxInterceptor(ctx)
		}
		pre(ctx, info.FullMethod, req)
		res, err := handler(ctx, req)
		post(ctx, res, err)
		return res, err
	}
	stream = func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		panic("unimplemented")
	}
	return
}
