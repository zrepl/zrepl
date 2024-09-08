// a tool to test whether a given transport implements the timeoutconn.Wire interface
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path"

	netssh "github.com/problame/go-netssh"
	"github.com/zrepl/yaml-config"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/transport"
	transportconfig "github.com/zrepl/zrepl/transport/fromconfig"
)

func noerror(err error) {
	if err != nil {
		panic(err)
	}
}

type Config struct {
	Connect config.ConnectEnum
	Serve   config.ServeEnum
}

var args struct {
	mode       string
	configPath string
	testCase   string
}

var conf Config

type TestCase interface {
	Client(wire transport.Wire)
	Server(wire transport.Wire)
}

func main() {
	flag.StringVar(&args.mode, "mode", "", "connect|serve")
	flag.StringVar(&args.configPath, "config", "", "config file path")
	flag.StringVar(&args.testCase, "testcase", "", "")
	flag.Parse()

	bytes, err := os.ReadFile(args.configPath)
	noerror(err)
	err = yaml.UnmarshalStrict(bytes, &conf)
	noerror(err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	global := &config.Global{
		Serve: &config.GlobalServe{
			StdinServer: &config.GlobalStdinServer{
				SockDir: "/tmp/wireevaluator_stdinserver",
			},
		},
	}

	switch args.mode {
	case "connect":
		tc, err := getTestCase(args.testCase)
		noerror(err)
		connecter, err := transportconfig.ConnecterFromConfig(global, conf.Connect, config.ParseFlagsNone)
		noerror(err)
		wire, err := connecter.Connect(ctx)
		noerror(err)
		tc.Client(wire)
	case "serve":
		tc, err := getTestCase(args.testCase)
		noerror(err)
		lf, err := transportconfig.ListenerFactoryFromConfig(global, conf.Serve, config.ParseFlagsNone)
		noerror(err)
		l, err := lf()
		noerror(err)
		conn, err := l.Accept(ctx)
		noerror(err)
		tc.Server(conn)
	case "stdinserver":
		identity := flag.Arg(0)
		unixaddr := path.Join(global.Serve.StdinServer.SockDir, identity)
		err := netssh.Proxy(ctx, unixaddr)
		if err == nil {
			os.Exit(0)
		}
		panic(err)
	default:
		panic(fmt.Sprintf("unknown mode %q", args.mode))
	}

}

func getTestCase(tcName string) (TestCase, error) {
	switch tcName {
	case "closewrite_server":
		return &CloseWrite{mode: CloseWriteServerSide}, nil
	case "closewrite_client":
		return &CloseWrite{mode: CloseWriteClientSide}, nil
	case "readdeadline_client":
		return &Deadlines{mode: DeadlineModeClientTimeout}, nil
	case "readdeadline_server":
		return &Deadlines{mode: DeadlineModeServerTimeout}, nil
	default:
		return nil, fmt.Errorf("unknown test case %q", tcName)
	}
}
