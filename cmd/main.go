package main

import (
	"errors"
	"fmt"
	"github.com/urfave/cli"
	"github.com/zrepl/zrepl/jobrun"
	"log"
	"os"
	"sync"
	"time"
)

var conf Config
var runner *jobrun.JobRunner
var logFlags int = log.LUTC | log.Ldate | log.Ltime

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

		jobrunLogger := log.New(os.Stderr, "jobrun ", logFlags)
		runner = jobrun.NewJobRunner(jobrunLogger)
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
	return
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
			RunFunc: func(log jobrun.Logger) error {
				log.Printf("%v: %#v\n", time.Now(), pull)
				time.Sleep(10 * time.Second)
				log.Printf("%v: %#v\n", time.Now(), pull)
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
			RunFunc: func(log jobrun.Logger) error {
				log.Printf("%v: %#v\n", time.Now(), push)
				return nil
			},
		}

		runner.AddJob(j)
	}

	wg.Wait()

	return nil
}
