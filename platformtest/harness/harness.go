package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/fatih/color"
	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/logging"
	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/platformtest"
	"github.com/zrepl/zrepl/platformtest/tests"
)

var bold = color.New(color.Bold)
var boldRed = color.New(color.Bold, color.FgHiRed)
var boldGreen = color.New(color.Bold, color.FgHiGreen)

var args struct {
	createArgs            platformtest.ZpoolCreateArgs
	stopAndKeepPoolOnFail bool

	run   string
	runRE *regexp.Regexp
}

func main() {
	if err := doMain(); err != nil {
		os.Exit(1)
	}
}

var exitWithErr = fmt.Errorf("exit with error")

func doMain() error {

	flag.StringVar(&args.createArgs.PoolName, "poolname", "", "")
	flag.StringVar(&args.createArgs.ImagePath, "imagepath", "", "")
	flag.Int64Var(&args.createArgs.ImageSize, "imagesize", 200*(1<<20), "")
	flag.StringVar(&args.createArgs.Mountpoint, "mountpoint", "", "")
	flag.BoolVar(&args.stopAndKeepPoolOnFail, "failure.stop-and-keep-pool", false, "if a test case fails, stop test execution and keep pool as it was when the test failed")
	flag.StringVar(&args.run, "run", "", "")
	flag.Parse()

	args.runRE = regexp.MustCompile(args.run)

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

	if err := args.createArgs.Validate(); err != nil {
		logger.Error(err.Error())
		panic(err)
	}
	ctx := platformtest.WithLogger(context.Background(), logger)
	ex := platformtest.NewEx(logger)

	type invocation struct {
		runFunc tests.Case
		result  *testCaseResult
	}

	invocations := make([]*invocation, 0, len(tests.Cases))
	for _, c := range tests.Cases {
		if args.runRE.MatchString(c.String()) {
			invocations = append(invocations, &invocation{runFunc: c})
		}
	}

	for _, inv := range invocations {

		bold.Printf("BEGIN TEST CASE %s\n", inv.runFunc.String())

		pool, err := platformtest.CreateOrReplaceZpool(ctx, ex, args.createArgs)
		if err != nil {
			panic(errors.Wrap(err, "create test pool"))
		}

		ctx := &platformtest.Context{
			Context:     ctx,
			RootDataset: filepath.Join(pool.Name(), "rootds"),
		}

		res := runTestCase(ctx, ex, inv.runFunc)
		inv.result = res
		if res.failed {
			fmt.Printf("%+v\n", res.failedStack) // print with stack trace
		}

		if res.failed && args.stopAndKeepPoolOnFail {
			boldRed.Printf("STOPPING TEST RUN AT FAILING TEST PER USER REQUEST\n")
			return exitWithErr
		}

		if err := pool.Destroy(ctx, ex); err != nil {
			panic(fmt.Sprintf("error destroying test pool: %s", err))
		}

		if res.failed {
			boldRed.Printf("TEST FAILED\n")
		} else if res.skipped {
			bold.Printf("TEST SKIPPED\n")
		} else if res.succeeded {
			boldGreen.Printf("TEST PASSED\n")
		} else {
			panic("unreachable")
		}

		fmt.Println()
	}

	var summary struct {
		succ, fail, skip []*invocation
	}
	for _, inv := range invocations {
		var bucket *[]*invocation
		if inv.result.failed {
			bucket = &summary.fail
		} else if inv.result.skipped {
			bucket = &summary.skip
		} else if inv.result.succeeded {
			bucket = &summary.succ
		} else {
			panic("unreachable")
		}
		*bucket = append(*bucket, inv)
	}
	printBucket := func(bucketName string, c *color.Color, bucket []*invocation) {
		c.Printf("%s:", bucketName)
		if len(bucket) == 0 {
			fmt.Printf(" []\n")
			return
		}
		fmt.Printf("\n")
		for _, inv := range bucket {
			fmt.Printf("  %s\n", inv.runFunc.String())
		}
	}
	printBucket("PASSING TESTS", boldGreen, summary.succ)
	printBucket("SKIPPED TESTS", bold, summary.skip)
	printBucket("FAILED TESTS", boldRed, summary.fail)

	if len(summary.fail) > 0 {
		return errors.New("at least one test failed")
	}
	return nil
}

type testCaseResult struct {
	// oneof
	failed, skipped, succeeded bool

	failedStack error // has stack inside, valid if failed=true
}

func runTestCase(ctx *platformtest.Context, ex platformtest.Execer, c tests.Case) *testCaseResult {

	// run case
	var paniced = false
	var panicValue interface{} = nil
	var panicStack error
	func() {
		defer func() {
			if item := recover(); item != nil {
				panicValue = item
				paniced = true
				panicStack = errors.Errorf("panic while running test: %v", panicValue)
			}
		}()
		c(ctx)
	}()

	if paniced {
		switch panicValue {
		case platformtest.SkipNowSentinel:
			return &testCaseResult{skipped: true}
		case platformtest.FailNowSentinel:
			return &testCaseResult{failed: true, failedStack: panicStack}
		default:
			return &testCaseResult{failed: true, failedStack: panicStack}
		}
	} else {
		return &testCaseResult{succeeded: true}
	}

}
