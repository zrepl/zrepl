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

var stdinserver struct {
	identity string
}

var StdinserverCmd = &cobra.Command{
	Use:   "stdinserver",
	Short: "start in stdin server mode (from authorized_keys file)",
	Run:   cmdStdinServer,
}

func init() {
	StdinserverCmd.Flags().StringVar(&stdinserver.identity, "identity", "", "")
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

	if stdinserver.identity == "" {
		err = fmt.Errorf("identity flag not set")
		return
	}
	identity := stdinserver.identity

	var sshByteStream io.ReadWriteCloser
	if sshByteStream, err = sshbytestream.Incoming(); err != nil {
		return
	}

	findMapping := func(cm []ClientMapping, identity string) zfs.DatasetMapping {
		for i := range cm {
			if cm[i].From == identity {
				return cm[i].Mapping
			}
		}
		return nil
	}
	sinkMapping := func(identity string) (sink zfs.DatasetMapping, err error) {
		if sink = findMapping(conf.Sinks, identity); sink == nil {
			return nil, fmt.Errorf("could not find sink for dataset")
		}
		return
	}

	sinkLogger := golog.New(logOut, fmt.Sprintf("sink[%s] ", identity), logFlags)
	handler := Handler{
		Logger:          sinkLogger,
		SinkMappingFunc: sinkMapping,
		PullACL:         findMapping(conf.PullACLs, identity),
	}

	if err = rpc.ListenByteStreamRPC(sshByteStream, identity, handler, sinkLogger); err != nil {
		log.Printf("listenbytestreamerror: %#v\n", err)
		os.Exit(1)
	}

	return

}
