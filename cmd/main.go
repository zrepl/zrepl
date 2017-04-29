package main

import (
	"errors"
	"fmt"
	"github.com/urfave/cli"
	"github.com/zrepl/zrepl/jobrun"
	"github.com/zrepl/zrepl/rpc"
	"github.com/zrepl/zrepl/sshbytestream"
	"io"
	"sync"
	"time"
)

type Role uint

const (
	ROLE_IPC    Role = iota
	ROLE_ACTION Role = iota
)

var conf Config
var handler Handler
var runner *jobrun.JobRunner

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

		runner = jobrun.NewJobRunner()
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

	// Do every pull, do every push
	// Scheduling

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runner.Start()
	}()

	for i := range conf.Pulls {
		pull := conf.Pulls[i]

		j := jobrun.Job{
			Name:     fmt.Sprintf("pull%d", i),
			Interval: time.Duration(5 * time.Second),
			Repeats:  true,
			RunFunc: func() error {
				fmt.Printf("%v: %#v\n", time.Now(), pull)
				time.Sleep(10 * time.Second)
				fmt.Printf("%v: %#v\n", time.Now(), pull)
				return nil
			},
		}

		runner.AddJob(j)
	}

	for i := range conf.Pushs {
		push := conf.Pushs[i]

		j := jobrun.Job{
			Name:     fmt.Sprintf("push%d", i),
			Interval: time.Duration(5 * time.Second),
			Repeats:  true,
			RunFunc: func() error {
				fmt.Printf("%v: %#v\n", time.Now(), push)
				return nil
			},
		}

		runner.AddJob(j)
	}

	wg.Wait()

	return nil
}
