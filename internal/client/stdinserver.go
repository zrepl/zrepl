package client

import (
	"os"

	"github.com/problame/go-netssh"

	"github.com/zrepl/zrepl/internal/cli"
	"github.com/zrepl/zrepl/internal/config"

	"context"
	"errors"
	"log"
	"path"
)

var StdinserverCmd = &cli.Subcommand{
	Use:   "stdinserver CLIENT_IDENTITY",
	Short: "stdinserver transport mode (started from authorized_keys file as forced command)",
	Run: func(ctx context.Context, subcommand *cli.Subcommand, args []string) error {
		return runStdinserver(subcommand.Config(), args)
	},
}

func runStdinserver(config *config.Config, args []string) error {

	// NOTE: the netssh proxying protocol requires exiting with non-zero status if anything goes wrong
	defer os.Exit(1)

	log := log.New(os.Stderr, "", log.LUTC|log.Ldate|log.Ltime)

	if len(args) != 1 || args[0] == "" {
		err := errors.New("must specify client_identity as positional argument")
		return err
	}

	identity := args[0]
	unixaddr := path.Join(config.Global.Serve.StdinServer.SockDir, identity)

	log.Printf("proxying client identity '%s' to zrepl daemon '%s'", identity, unixaddr)

	ctx := netssh.ContextWithLog(context.TODO(), log)

	err := netssh.Proxy(ctx, unixaddr)
	if err == nil {
		log.Print("proxying finished successfully, exiting with status 0")
		os.Exit(0)
	}
	log.Printf("error proxying: %s", err)
	return nil
}
