package client

import (
	"fmt"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon"
	"github.com/zrepl/zrepl/version"
	"os"
)

type VersionArgs struct {
	Show   string
	Config *config.Config
}

func RunVersion(args VersionArgs) {

	die := func() {
		fmt.Fprintf(os.Stderr, "exiting after error\n")
		os.Exit(1)
	}

	if args.Show != "daemon" && args.Show != "client" && args.Show != "" {
		fmt.Fprintf(os.Stderr, "show flag must be 'client' or 'server' or be left empty")
		die()
	}

	var clientVersion, daemonVersion *version.ZreplVersionInformation
	if args.Show == "client" || args.Show == "" {
		clientVersion = version.NewZreplVersionInformation()
		fmt.Printf("client: %s\n", clientVersion.String())
	}
	if args.Show == "daemon" || args.Show == "" {

		httpc, err := controlHttpClient(args.Config.Global.Control.SockPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "server: error: %s\n", err)
			die()
		}

		var info version.ZreplVersionInformation
		err = jsonRequestResponse(httpc, daemon.ControlJobEndpointVersion, "", &info)
		if err != nil {
			fmt.Fprintf(os.Stderr, "server: error: %s\n", err)
			die()
		}
		daemonVersion = &info
		fmt.Printf("server: %s\n", daemonVersion.String())
	}

	if args.Show == "" {
		if clientVersion.Version != daemonVersion.Version {
			fmt.Fprintf(os.Stderr, "WARNING: client version != daemon version, restart zrepl daemon\n")
		}
	}

}
