package main

import (
	"errors"
	"fmt"
	"github.com/urfave/cli"
	"github.com/zrepl/zrepl/rpc"
	"github.com/zrepl/zrepl/sshbytestream"
	"io"
)

type Role uint

const (
	ROLE_IPC    Role = iota
	ROLE_ACTION Role = iota
)

var conf Config
var handler Handler

func main() {

	app := cli.NewApp()

	app.Name = "zrepl"
	app.Usage = "replicate zfs datasets"
	app.EnableBashCompletion = true
	app.Flags = []cli.Flag{
		cli.StringFlag{Name: "config"},
	}
	app.Before = func(c *cli.Context) (err error) {
		if !c.GlobalIsSet("config") {
			return errors.New("config flag not set")
		}
		if conf, err = ParseConfig(c.GlobalString("config")); err != nil {
			return
		}
		handler = Handler{}
		return
	}
	app.Commands = []cli.Command{
		{
			Name:    "sink",
			Aliases: []string{"s"},
			Usage:   "start in sink mode",
			Flags: []cli.Flag{
				cli.StringFlag{Name: "identity"},
			},
			Action: doSink,
		},
		{
			Name:    "run",
			Aliases: []string{"r"},
			Usage:   "do replication",
			Action:  doRun,
		},
	}

	app.RunAndExitOnError()

}

func doSink(c *cli.Context) (err error) {

	var sshByteStream io.ReadWriteCloser
	if sshByteStream, err = sshbytestream.Incoming(); err != nil {
		return
	}

	return rpc.ListenByteStreamRPC(sshByteStream, handler)
}

func doRun(c *cli.Context) error {

	fmt.Printf("%#v", conf)

	return nil
}
