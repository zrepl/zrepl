package client

import (
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon"
	"log"
	"os"
)

type PProfArgs struct {
	daemon.PprofServerControlMsg
}

func RunPProf(conf *config.Config, args PProfArgs) {
	log := log.New(os.Stderr, "", 0)

	die := func() {
		log.Printf("exiting after error")
		os.Exit(1)
	}

	log.Printf("connecting to zrepl daemon")

	httpc, err := controlHttpClient(conf.Global.Control.SockPath)
	if err != nil {
		log.Printf("error creating http client: %s", err)
		die()
	}
	err = jsonRequestResponse(httpc, daemon.ControlJobEndpointPProf, args.PprofServerControlMsg, struct{}{})
	if err != nil {
		log.Printf("error sending control message: %s", err)
		die()
	}
	log.Printf("finished")
}
