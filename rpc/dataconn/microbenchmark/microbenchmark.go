// microbenchmark to manually test rpc/dataconn perforamnce
//
// With stdin / stdout on client and server, simulating zfs send|recv piping
//
//   ./microbenchmark -appmode server  | pv -r > /dev/null
//   ./microbenchmark -appmode client -direction recv < /dev/zero
//
//
// Without the overhead of pipes (just protocol perforamnce, mostly useful with perf bc no bw measurement)
//
//   ./microbenchmark -appmode client -direction recv -devnoopWriter -devnoopReader
//   ./microbenchmark -appmode server -devnoopReader -devnoopWriter
//
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"

	"github.com/pkg/profile"

	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/replication/logic/pdu"
	"github.com/zrepl/zrepl/rpc/dataconn"
	"github.com/zrepl/zrepl/rpc/dataconn/timeoutconn"
	"github.com/zrepl/zrepl/transport"
	"github.com/zrepl/zrepl/util/devnoop"
	"github.com/zrepl/zrepl/zfs"
)

func orDie(err error) {
	if err != nil {
		panic(err)
	}
}

type readerStreamCopier struct{ io.Reader }

func (readerStreamCopier) Close() error { return nil }

type readerStreamCopierErr struct {
	error
}

func (readerStreamCopierErr) IsReadError() bool  { return false }
func (readerStreamCopierErr) IsWriteError() bool { return true }

func (c readerStreamCopier) WriteStreamTo(w io.Writer) zfs.StreamCopierError {
	var buf [1 << 21]byte
	_, err := io.CopyBuffer(w, c.Reader, buf[:])
	// always assume write error
	return readerStreamCopierErr{err}
}

type devNullHandler struct{}

func (devNullHandler) Send(ctx context.Context, r *pdu.SendReq) (*pdu.SendRes, zfs.StreamCopier, error) {
	var res pdu.SendRes
	if args.devnoopReader {
		return &res, readerStreamCopier{devnoop.Get()}, nil
	} else {
		return &res, readerStreamCopier{os.Stdin}, nil
	}
}

func (devNullHandler) Receive(ctx context.Context, r *pdu.ReceiveReq, stream zfs.StreamCopier) (*pdu.ReceiveRes, error) {
	var out io.Writer = os.Stdout
	if args.devnoopWriter {
		out = devnoop.Get()
	}
	err := stream.WriteStreamTo(out)
	var res pdu.ReceiveRes
	return &res, err
}

func (devNullHandler) PingDataconn(ctx context.Context, r *pdu.PingReq) (*pdu.PingRes, error) {
	return &pdu.PingRes{
		Echo: r.GetMessage(),
	}, nil
}

type tcpConnecter struct {
	addr string
}

func (c tcpConnecter) Connect(ctx context.Context) (timeoutconn.Wire, error) {
	conn, err := net.Dial("tcp", c.addr)
	if err != nil {
		return nil, err
	}
	return conn.(*net.TCPConn), nil
}

type tcpListener struct {
	nl          *net.TCPListener
	clientIdent string
}

func (l tcpListener) Accept(ctx context.Context) (*transport.AuthConn, error) {
	tcpconn, err := l.nl.AcceptTCP()
	orDie(err)
	return transport.NewAuthConn(tcpconn, l.clientIdent), nil
}

func (l tcpListener) Addr() net.Addr { return l.nl.Addr() }

func (l tcpListener) Close() error { return l.nl.Close() }

var args struct {
	addr          string
	appmode       string
	direction     string
	profile       bool
	devnoopReader bool
	devnoopWriter bool
}

func server() {

	log := logger.NewStderrDebugLogger()
	log.Debug("starting server")
	nl, err := net.Listen("tcp", args.addr)
	orDie(err)
	l := tcpListener{nl.(*net.TCPListener), "fakeclientidentity"}

	srv := dataconn.NewServer(nil, logger.NewStderrDebugLogger(), devNullHandler{})

	ctx := context.Background()

	srv.Serve(ctx, l)

}

func main() {

	flag.BoolVar(&args.profile, "profile", false, "")
	flag.BoolVar(&args.devnoopReader, "devnoopReader", false, "")
	flag.BoolVar(&args.devnoopWriter, "devnoopWriter", false, "")
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

	connecter := tcpConnecter{args.addr}
	client := dataconn.NewClient(connecter, logger)

	switch args.direction {
	case "send":
		req := pdu.SendReq{}
		_, stream, err := client.ReqSend(ctx, &req)
		orDie(err)
		err = stream.WriteStreamTo(os.Stdout)
		orDie(err)
	case "recv":
		var r io.Reader = os.Stdin
		if args.devnoopReader {
			r = devnoop.Get()
		}
		s := readerStreamCopier{r}
		req := pdu.ReceiveReq{}
		_, err := client.ReqRecv(ctx, &req, &s)
		orDie(err)
	default:
		orDie(fmt.Errorf("unknown direction%q", args.direction))
	}

}
