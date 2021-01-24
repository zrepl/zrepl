// This package demonstrates how the grpcclientidentity package can be used
// to set up a gRPC greeter service.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/rpc/grpcclientidentity/example/pdu"
	"github.com/zrepl/zrepl/rpc/grpcclientidentity/grpchelper"
	"github.com/zrepl/zrepl/transport/tcp"
)

var args struct {
	mode string
}

var log = logger.NewStderrDebugLogger()

func main() {
	flag.StringVar(&args.mode, "mode", "", "client|server")
	flag.Parse()

	switch args.mode {
	case "client":
		client()
	case "server":
		server()
	default:
		log.Printf("unknown mode %q")
		os.Exit(1)
	}
}

func onErr(err error, format string, args ...interface{}) {
	log.WithError(err).Error(fmt.Sprintf("%s: %s", fmt.Sprintf(format, args...), err))
	os.Exit(1)
}

func client() {
	cn, err := tcp.TCPConnecterFromConfig(&config.TCPConnect{
		ConnectCommon: config.ConnectCommon{
			Type: "tcp",
		},
		Address:     "127.0.0.1:8080",
		DialTimeout: 10 * time.Second,
	})
	if err != nil {
		onErr(err, "build connecter error")
	}

	clientConn := grpchelper.ClientConn(cn, log)
	defer clientConn.Close()

	// normal usage from here on

	client := pdu.NewGreeterClient(clientConn)
	resp, err := client.Greet(context.Background(), &pdu.GreetRequest{Name: "somethingimadeup"})
	if err != nil {
		onErr(err, "RPC error")
	}

	fmt.Printf("got response:\n\t%s\n", resp.GetMsg())
}

const clientIdentityKey = "clientIdentity"

func server() {
	authListenerFactory, err := tcp.TCPListenerFactoryFromConfig(nil, &config.TCPServe{
		ServeCommon: config.ServeCommon{
			Type: "tcp",
		},
		Listen: "127.0.0.1:8080",
		Clients: map[string]string{
			"127.0.0.1": "localclient",
			"::1":       "localclient",
		},
	})
	if err != nil {
		onErr(err, "cannot build listener factory")
	}

	log := logger.NewStderrDebugLogger()

	authListener, err := authListenerFactory()
	if err != nil {
		onErr(err, "cannot listen")
	}

	srv, serve := grpchelper.NewServer(authListener, clientIdentityKey, log, nil)

	svc := &greeter{prepend: "hello "}
	pdu.RegisterGreeterServer(srv, svc)

	if err := serve(); err != nil {
		onErr(err, "error serving")
	}
}

type greeter struct {
	pdu.UnsafeGreeterServer
	prepend string
}

func (g *greeter) Greet(ctx context.Context, r *pdu.GreetRequest) (*pdu.GreetResponse, error) {
	ci, _ := ctx.Value(clientIdentityKey).(string)
	log.WithField("clientIdentity", ci).Info("Greet() request") // show that we got the client identity
	return &pdu.GreetResponse{Msg: fmt.Sprintf("%s%s (clientIdentity=%q)", g.prepend, r.GetName(), ci)}, nil
}
