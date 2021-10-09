package main

import (
	"context"
	"flag"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/transport/tls"
)

var servConf = config.TLSServe{
	ServeCommon: config.ServeCommon{
		Type: "tls",
	},
	HandshakeTimeout: 10 * time.Second,
}

var clientConf = config.TLSConnect{
	ConnectCommon: config.ConnectCommon{
		Type: "",
	},
	Address:     "",
	Ca:          "",
	Cert:        "",
	Key:         "",
	ServerCN:    "",
	DialTimeout: 10 * time.Second,
}

var ca string
var mode string

func main() {

	flag.StringVar(&mode, "mode", "", "server|client")

	flag.StringVar(&ca, "ca", "", "path")

	flag.StringVar(&servConf.Listen, "serve.listen", "", "")
	flag.StringVar(&servConf.Cert, "serve.cert", "", "path")
	flag.StringVar(&servConf.Key, "serve.key", "", "path")
	var clientCN string
	flag.StringVar(&clientCN, "serve.client_cn", "", "")

	flag.StringVar(&clientConf.Address, "client.address", "", "")
	flag.StringVar(&clientConf.Cert, "client.cert", "", "path")
	flag.StringVar(&clientConf.Key, "client.key", "", "path")
	flag.StringVar(&clientConf.ServerCN, "client.server_cn", "", "")

	flag.Parse()

	servConf.ClientCNs = append(servConf.ClientCNs, clientCN)

	servConf.Ca = ca
	clientConf.Ca = ca

	switch mode {
	case "server":
		server()
	case "client":
		client()
	default:
		panic(mode)
	}

}

func server() {

	servFactory, err := tls.TLSListenerFactoryFromConfig(nil, &servConf)
	if err != nil {
		panic(err)
	}

	listener, err := servFactory()
	if err != nil {
		panic(err)
	}

	ctx := context.Background()

	for {
		conn, err := listener.Accept(ctx)
		if err != nil {
			log.Printf("accept error: %s", err)
			continue
		}
		go func() {
			defer conn.Close()

			log.Printf("handling connection %s", conn.RemoteAddr())
			_, err = io.Copy(conn, strings.NewReader("here is the server\n"))
			if err != nil {
				log.Printf("%s: respond to client error: %s", conn.RemoteAddr(), err)
				return
			}

			err = conn.CloseWrite()
			if err != nil {
				log.Printf("%s: failed to close write connection: err", conn.RemoteAddr(), err)
			}

			log.Printf("%s: waiting for client to close connection", conn.RemoteAddr())

			_, err = io.Copy(io.Discard, conn)
			if err != nil {
				log.Printf("%s: error draining client connection: %s", conn.RemoteAddr(), err)
				return
			}

			log.Printf("%s: done", conn.RemoteAddr())
			return
		}()
	}

}

func client() {
	connecter, err := tls.TLSConnecterFromConfig(&clientConf)
	if err != nil {
		panic(err)
	}

	ctx := context.Background()
	conn, err := connecter.Connect(ctx)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	_, err = io.Copy(os.Stdout, conn)
	if err != nil {
		panic(err)
	}
}
