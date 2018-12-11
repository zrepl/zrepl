package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"

	"github.com/pkg/profile"
	"github.com/zrepl/zrepl/rpc/dataconn"
	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/replication/pdu"
)

func orDie(err error) {
	if err != nil {
		panic(err)
	}
}

type devNullHandler struct{}

func (devNullHandler) Send(ctx context.Context, r *pdu.SendReq) (*pdu.SendRes, io.ReadCloser, error) {
	var res pdu.SendRes
	return &res, os.Stdin, nil
}

func (devNullHandler) Receive(ctx context.Context, r *pdu.ReceiveReq, stream io.Reader) (*pdu.ReceiveRes, error) {
	var buf [1<<15]byte
	_, err := io.CopyBuffer(os.Stdout, stream, buf[:])
	var res pdu.ReceiveRes
	return &res, err
}

type tcpConnecter struct {
	net, addr string
}

func (c tcpConnecter) Connect(ctx context.Context) (net.Conn, error) {
	return net.Dial(c.net, c.addr)
}

var args struct {
	addr      string
	appmode   string
	direction string
	profile   bool
}

func server() {

	log := logger.NewStderrDebugLogger()
	log.Debug("starting server")
	l, err := net.Listen("tcp", args.addr)
	orDie(err)

	srvConfig := dataconn.ServerConfig{
		Shared: dataconn.SharedConfig {
			MaxProtoLen:      4096,
			MaxHeaderLen:     4096,
			SendChunkSize:    1 << 17,
			MaxRecvChunkSize: 1 << 17,
		},
	}
	srv := dataconn.NewServer(devNullHandler{}, srvConfig, nil)

	ctx := context.Background()
	ctx = dataconn.WithLogger(ctx, log)
	srv.Serve(ctx, l)

}

func main() {

	flag.BoolVar(&args.profile, "profile", false, "")
	flag.StringVar(&args.addr, "address", ":8888", "")
	flag.StringVar(&args.appmode, "appmode", "client|server", "")
	flag.StringVar(&args.direction, "direction", "", "send|recv")
	flag.Parse()

	if args.profile {
		defer profile.Start(profile.CPUProfile).Stop()
	}

	switch args.appmode {
	case "client":
		client()
	case "server":
		server()
	default:
		orDie(fmt.Errorf("unknown appmode %q", args.appmode))
	}
}

func client() {

	logger := logger.NewStderrDebugLogger()
	ctx := context.Background()
	ctx = dataconn.WithLogger(ctx, logger)

	clientConfig := dataconn.ClientConfig{
		Shared: dataconn.SharedConfig {
			MaxProtoLen:      4096,
			MaxHeaderLen:     4096,
			SendChunkSize:    1 << 17,
			MaxRecvChunkSize: 1 << 17,
		},
	}
	orDie(clientConfig.Validate())

	connecter := tcpConnecter{"tcp", args.addr}
	client := dataconn.NewClient(connecter, clientConfig)

	switch args.direction {
	case "send":
		req := pdu.SendReq{}
		_, stream, err := client.ReqSendStream(ctx, &req)
		orDie(err)
		var buf [1<<15]byte
		_, err = io.CopyBuffer(os.Stdout, stream, buf[:])
		orDie(err)
	case "recv":
		var buf bytes.Buffer
		buf.WriteString("teststreamtobereceived")
		req := pdu.ReceiveReq{}
		_, err := client.ReqRecv(ctx, &req, os.Stdin)
		orDie(err)
	default:
		orDie(fmt.Errorf("unknown direction%q", args.direction))
	}

}
