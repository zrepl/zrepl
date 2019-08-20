package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/fatih/color"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/logging"
	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/platformtest"
	"github.com/zrepl/zrepl/platformtest/tests"
)

func main() {

	root := flag.String("root", "", "empty root filesystem under which we conduct the platform test")
	flag.Parse()
	if *root == "" {
		panic(*root)
	}

	outlets := logger.NewOutlets()
	outlet, level, err := logging.ParseOutlet(config.LoggingOutletEnum{Ret: &config.StdoutLoggingOutlet{
		LoggingOutletCommon: config.LoggingOutletCommon{
			Level:  "debug",
			Format: "human",
		},
	}})
	if err != nil {
		panic(err)
	}
	outlets.Add(outlet, level)
	logger := logger.NewLogger(outlets, 1*time.Second)

	ctx := &platformtest.Context{
		Context:     platformtest.WithLogger(context.Background(), logger),
		RootDataset: *root,
	}

	bold := color.New(color.Bold)
	for _, c := range tests.Cases {
		bold.Printf("BEGIN TEST CASE %s\n", c)
		c(ctx)
		bold.Printf("DONE  TEST CASE %s\n", c)
		fmt.Println()
	}

}
