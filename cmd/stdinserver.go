package cmd

import (
	"os"

	"context"
	"github.com/problame/go-netssh"
	"github.com/spf13/cobra"
	"log"
	"path"
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

	// NOTE: the netssh proxying protocol requires exiting with non-zero status if anything goes wrong
	defer os.Exit(1)

	log := log.New(os.Stderr, "", log.LUTC|log.Ldate|log.Ltime)

	conf, err := ParseConfig(rootArgs.configFile)
	if err != nil {
		log.Printf("error parsing config: %s", err)
		return
	}

	if len(args) != 1 || args[0] == "" {
		log.Print("must specify client_identity as positional argument")
		return
	}

	identity := args[0]
	unixaddr := path.Join(conf.Global.Serve.Stdinserver.SockDir, identity)

	log.Printf("proxying client identity '%s' to zrepl daemon '%s'", identity, unixaddr)

	ctx := netssh.ContextWithLog(context.TODO(), log)

	err = netssh.Proxy(ctx, unixaddr)
	if err == nil {
		log.Print("proxying finished successfully, exiting with status 0")
		os.Exit(0)
	}
	log.Printf("error proxying: %s", err)

}
