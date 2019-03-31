// This integration test exercises the behavior of heartbeatconn
// where the server is slow at handling the data received over the connection.
// Note that the server is still sending heartbeats to the client, it's just the
// data handling (usually I/O in case of zrepl endpoint.Receiver) that is slow.
//
// In commit 082335df5d85e1b0b9faa35ff182c71886142d3e and earlier, heartbeatconn would fail
// this benchmark with a writev I/O timeout (here the ss(8) output at the time of failure)
//
//  ESTAB        33369    0                                        127.0.0.1:12345                                                127.0.0.1:57282                    users:(("heartbeatconn_i",pid=25953,fd=5))
//	 cubic wscale:7,7 rto:203 rtt:2.992/5.849 ato:162 mss:32768 pmtu:65535 rcvmss:32741 advmss:65483 cwnd:10 bytes_sent:48 bytes_acked:48 bytes_received:195401 segs_out:44 segs_in:57 data_segs_out:6 data_segs_in:34 send 876.1Mbps lastsnd:125 lastrcv:9390 lastack:125 pacing_rate 1752.0Mbps delivery_rate 6393.8Mbps delivered:7 app_limited busy:42ms rcv_rtt:1 rcv_space:65483 rcv_ssthresh:65483 minrtt:0.029
//	 --
//	 ESTAB        0        3956805                                  127.0.0.1:57282                                                127.0.0.1:12345                    users:(("heartbeatconn_i",pid=26100,fd=3))
//		  cubic wscale:7,7 rto:211 backoff:5 rtt:10.38/16.937 ato:40 mss:32768 pmtu:65535 rcvmss:536 advmss:65483 cwnd:10 bytes_sent:195401 bytes_acked:195402 bytes_received:48 segs_out:57 segs_in:45 data_segs_out:34 data_segs_in:6 send 252.5Mbps lastsnd:9390 lastrcv:125 lastack:125 pacing_rate 505.1Mbps delivery_rate 1971.0Mbps delivered:35 busy:30127ms rwnd_limited:30086ms(99.9%) rcv_space:65495 rcv_ssthresh:65495 notsent:3956805 minrtt:0.007
//	 panic: writev tcp 127.0.0.1:57282->127.0.0.1:12345: i/o timeout
//
// The assumed reason for those writev timeouts is the following:
// - Sporadic server stalls (sever data handling, usually I/O) cause TCP exponential backoff on the client for client->server
// - Go runtime unblocks after the deadline expires, resultin gin writev I/O timeout
// - That is, even though the client observed heartbeats from the server
// -> TCP doesn't assume symmetric connection behavior, but our implementation does.
//
// The fix contained in the commit this message was committed with resets the deadline whenever
// a heartbeat is received from the server.
//
//
// How to run this integration test:
//
//
//  Terminal 1:
//     $ ZREPL_RPC_DATACONN_HEARTBEATCONN_DEBUG=1 go run heartbeatconn_integration_variablereceiverate.go  -mode server -addr 127.0.0.1:12345
//     rpc/dataconn/heartbeatconn: send heartbeat
//     rpc/dataconn/heartbeatconn: send heartbeat
//     ...
//
//  Terminal 2:
//     $ ZREPL_RPC_DATACONN_HEARTBEATCONN_DEBUG=1 go run heartbeatconn_integration_variablereceiverate.go  -mode  client -addr 127.0.0.1:12345
//     rpc/dataconn/heartbeatconn: received heartbeat, resetting write timeout
//     rpc/dataconn/heartbeatconn: renew frameconn write timeout returned errT=<nil> err=%!s(<nil>)
//     rpc/dataconn/heartbeatconn: send heartbeat
//     rpc/dataconn/heartbeatconn: received heartbeat, resetting write timeout
//     rpc/dataconn/heartbeatconn: renew frameconn write timeout returned errT=<nil> err=%!s(<nil>)
//     rpc/dataconn/heartbeatconn: received heartbeat, resetting write timeout
//     ...
//
//  You should observe
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path"
	"time"

	"github.com/zrepl/zrepl/util/devnoop"

	"github.com/zrepl/zrepl/rpc/dataconn/heartbeatconn"
)

func orDie(err error) {
	if err != nil {
		grepfield := path.Base(os.Args[0])[:10]
		fmt.Fprintf(os.Stderr, "grepping for %s\n", grepfield)
		sh := fmt.Sprintf("ss -ntpi | grep -A1 %s", grepfield)
		cmd := exec.Command("bash", "-c", sh)
		o, _ := cmd.CombinedOutput()
		buf := bytes.NewBuffer(o)
		_, _ = io.Copy(os.Stderr, buf)
		panic(err)
	}
}

var mode string
var addr string

func main() {

	flag.StringVar(&mode, "mode", "", "server|client")
	flag.StringVar(&addr, "addr", "INVALID", "")
	flag.Parse()

	modemap := map[string]func(){
		"server": server,
		"client": client,
	}
	modemap[mode]()

}

func server() {
	ln, err := net.Listen("tcp", addr)
	orDie(err)
	l := ln.(*net.TCPListener)
	for {
		c, err := l.AcceptTCP()
		if err != nil {
			log.Printf("accept err: %s", err)
			continue
		}
		hc := heartbeatconn.Wrap(c, 5*time.Second, 10*time.Second)

		for {
			f, err := hc.ReadFrame()
			orDie(err)
			// _, err = buf.Write(f.Buffer.Bytes())
			// orDie(err)
			sleep := time.Duration(rand.NormFloat64()*500) * time.Millisecond
			time.Sleep(sleep)
			f.Buffer.Free()
		}

	}

}

func client() {
	c, err := net.Dial("tcp", addr)
	orDie(err)
	hc := heartbeatconn.Wrap(c.(*net.TCPConn), 5*time.Second, 10*time.Second)

	// follow API requirements to always ReadFrame
	go func() {
		for {
			f, err := hc.ReadFrame()
			orDie(err)
			f.Buffer.Free()
		}
	}()

	dn := devnoop.Get()
	var buf [1 << 10]byte
	for {
		n, err := dn.Read(buf[:])
		orDie(err)
		err = hc.WriteFrame(buf[:n], 23)
		orDie(err)
	}
}
