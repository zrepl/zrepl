package cmd

import (
	"fmt"
	"os"

	"context"
	"github.com/ftrvxmtrx/fd"
	"github.com/spf13/cobra"
	"io"
	"log"
	"net"
)

var StdinserverCmd = &cobra.Command{
	Use:   "stdinserver CLIENT_IDENTITY",
	Short: "start in stdinserver mode (from authorized_keys file)",
	Run:   cmdStdinServer,
}

func init() {
	RootCmd.AddCommand(StdinserverCmd)
}

func cmdStdinServer(cmd *cobra.Command, args []string) {

	log := log.New(os.Stderr, "", log.LUTC|log.Ldate|log.Ltime)

	die := func() {
		log.Printf("stdinserver exiting after fatal error")
		os.Exit(1)
	}

	ctx := context.WithValue(context.Background(), contextKeyLog, log)
	conf, err := ParseConfig(ctx, rootArgs.configFile)
	if err != nil {
		log.Printf("error parsing config: %s", err)
		die()
	}

	if len(args) != 1 || args[0] == "" {
		err = fmt.Errorf("must specify client_identity as positional argument")
		die()
	}
	identity := args[0]

	unixaddr, err := stdinserverListenerSocket(conf.Global.Serve.Stdinserver.SockDir, identity)
	if err != nil {
		log.Printf("%s", err)
		os.Exit(1)
	}

	log.Printf("opening control connection to zrepld via %s", unixaddr)
	conn, err := net.DialUnix("unix", nil, unixaddr)
	if err != nil {
		log.Printf("error connecting to zrepld: %s", err)
		die()
	}

	log.Printf("sending stdin and stdout fds to zrepld")
	err = fd.Put(conn, os.Stdin, os.Stdout)
	if err != nil {
		log.Printf("error: %s", err)
		die()
	}

	log.Printf("waiting for zrepld to close control connection")
	for {

		var buf [64]byte
		n, err := conn.Read(buf[:])
		if err == nil && n != 0 {
			log.Printf("protocol error: read expected to timeout or EOF returned bytes")
		}

		if err == io.EOF {
			log.Printf("zrepld closed control connection, terminating")
			break
		}

		neterr, ok := err.(net.Error)
		if !ok {
			log.Printf("received unexpected error type: %T %s", err, err)
			die()
		}
		if !neterr.Timeout() {
			log.Printf("receivd unexpected net.Error (not a timeout): %s", neterr)
			die()
		}
		// Read timed out, as expected
	}

	return

}
