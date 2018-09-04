package client

import (
	"os"

	"context"
	"github.com/problame/go-netssh"
	"log"
	"path"
	"github.com/zrepl/zrepl/config"
	"errors"
)


func RunStdinserver(config *config.Config, args []string) error {

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
