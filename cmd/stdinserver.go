package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"github.com/zrepl/zrepl/rpc"
	"github.com/zrepl/zrepl/sshbytestream"
	"github.com/zrepl/zrepl/zfs"
	"io"
	golog "log"
	"os"
)

var StdinserverCmd = &cobra.Command{
	Use:   "stdinserver CLIENT_IDENTITY",
	Short: "start in stdin server mode (from authorized_keys file)",
	Run:   cmdStdinServer,
}

func init() {
	RootCmd.AddCommand(StdinserverCmd)
}

func cmdStdinServer(cmd *cobra.Command, args []string) {

	var err error
	defer func() {
		if err != nil {
			log.Printf("stdinserver exiting with error: %s", err)
			os.Exit(1)
		}
	}()

	if len(args) != 1 || args[0] == "" {
		err = fmt.Errorf("must specify client identity as positional argument")
		return
	}
	identity := args[0]

	pullACL := conf.PullACLs[identity]
	if pullACL == nil {
		err = fmt.Errorf("could not find PullACL for identity '%s'", identity)
		return
	}

	var sshByteStream io.ReadWriteCloser
	if sshByteStream, err = sshbytestream.Incoming(); err != nil {
		return
	}

	sinkMapping := func(identity string) (m zfs.DatasetMapping, err error) {
		sink := conf.Sinks[identity]
		if sink == nil {
			return nil, fmt.Errorf("could not find sink for dataset")
		}
		return sink.Mapping, nil
	}

	sinkLogger := golog.New(logOut, fmt.Sprintf("sink[%s] ", identity), logFlags)
	handler := Handler{
		Logger:          sinkLogger,
		SinkMappingFunc: sinkMapping,
		PullACL:         pullACL.Mapping,
	}

	if err = rpc.ListenByteStreamRPC(sshByteStream, identity, handler, sinkLogger); err != nil {
		log.Printf("listenbytestreamerror: %#v\n", err)
		os.Exit(1)
	}

	return

}
