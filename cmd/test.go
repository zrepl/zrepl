package cmd

import (
	"os"

	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/kr/pretty"
	"github.com/spf13/cobra"
	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/zfs"
	"time"
)

var testCmd = &cobra.Command{
	Use:              "test",
	Short:            "test configuration",
	PersistentPreRun: testCmdGlobalInit,
}

var testCmdGlobal struct {
	log  Logger
	conf *Config
}

var testConfigSyntaxCmd = &cobra.Command{
	Use:   "config",
	Short: "parse config file and dump parsed datastructure",
	Run:   doTestConfig,
}

var testDatasetMapFilter = &cobra.Command{
	Use:     "pattern jobname test/zfs/dataset/path",
	Short:   "test dataset mapping / filter specified in config",
	Example: ` zrepl test pattern my_pull_job tank/tmp`,
	Run:     doTestDatasetMapFilter,
}

var testPrunePolicyArgs struct {
	side        PrunePolicySide
	showKept    bool
	showRemoved bool
}

var testPrunePolicyCmd = &cobra.Command{
	Use:   "prune jobname",
	Short: "do a dry-run of the pruning part of a job",
	Run:   doTestPrunePolicy,
}

func init() {
	RootCmd.AddCommand(testCmd)
	testCmd.AddCommand(testConfigSyntaxCmd)
	testCmd.AddCommand(testDatasetMapFilter)

	testPrunePolicyCmd.Flags().VarP(&testPrunePolicyArgs.side, "side", "s", "prune_lhs (left) or prune_rhs (right)")
	testPrunePolicyCmd.Flags().BoolVar(&testPrunePolicyArgs.showKept, "kept", false, "show kept snapshots")
	testPrunePolicyCmd.Flags().BoolVar(&testPrunePolicyArgs.showRemoved, "removed", true, "show removed snapshots")
	testCmd.AddCommand(testPrunePolicyCmd)
}

func testCmdGlobalInit(cmd *cobra.Command, args []string) {

	out := logger.NewOutlets()
	out.Add(WriterOutlet{&NoFormatter{}, os.Stdout}, logger.Info)
	log := logger.NewLogger(out, 1*time.Second)
	testCmdGlobal.log = log

	var err error
	if testCmdGlobal.conf, err = ParseConfig(rootArgs.configFile); err != nil {
		testCmdGlobal.log.Printf("error parsing config file: %s", err)
		os.Exit(1)
	}

}

func doTestConfig(cmd *cobra.Command, args []string) {

	log, conf := testCmdGlobal.log, testCmdGlobal.conf

	log.Printf("config ok")
	log.Printf("%# v", pretty.Formatter(conf))
	return
}

func doTestDatasetMapFilter(cmd *cobra.Command, args []string) {

	log, conf := testCmdGlobal.log, testCmdGlobal.conf

	if len(args) != 2 {
		log.Printf("specify job name as first postitional argument, test input as second")
		log.Printf(cmd.UsageString())
		os.Exit(1)
	}
	n, i := args[0], args[1]

	jobi, err := conf.LookupJob(n)
	if err != nil {
		log.Printf("%s", err)
		os.Exit(1)
	}

	var mf *DatasetMapFilter
	switch j := jobi.(type) {
	case *PullJob:
		mf = j.Mapping
	case *SourceJob:
		mf = j.Filesystems
	case *LocalJob:
		mf = j.Mapping
	default:
		panic("incomplete implementation")
	}

	ip, err := zfs.NewDatasetPath(i)
	if err != nil {
		log.Printf("cannot parse test input as ZFS dataset path: %s", err)
		os.Exit(1)
	}

	if mf.filterMode {
		pass, err := mf.Filter(ip)
		if err != nil {
			log.Printf("error evaluating filter: %s", err)
			os.Exit(1)
		}
		log.Printf("filter result: %v", pass)
	} else {
		res, err := mf.Map(ip)
		if err != nil {
			log.Printf("error evaluating mapping: %s", err)
			os.Exit(1)
		}
		toStr := "NO MAPPING"
		if res != nil {
			toStr = res.ToString()
		}
		log.Printf("%s => %s", ip.ToString(), toStr)

	}

}

func doTestPrunePolicy(cmd *cobra.Command, args []string) {

	log, conf := testCmdGlobal.log, testCmdGlobal.conf

	if cmd.Flags().NArg() != 1 {
		log.Printf("specify job name as first positional argument")
		log.Printf(cmd.UsageString())
		os.Exit(1)
	}

	jobname := cmd.Flags().Arg(0)
	jobi, err := conf.LookupJob(jobname)
	if err != nil {
		log.Printf("%s", err)
		os.Exit(1)
	}

	jobp, ok := jobi.(PruningJob)
	if !ok {
		log.Printf("job doesn't do any prunes")
		os.Exit(0)
	}

	log.Printf("job dump:\n%s", pretty.Sprint(jobp))

	pruner, err := jobp.Pruner(testPrunePolicyArgs.side, true)
	if err != nil {
		log.Printf("cannot create test pruner: %s", err)
		os.Exit(1)
	}

	log.Printf("start pruning")

	ctx := WithLogger(context.Background(), log)
	result, err := pruner.Run(ctx)
	if err != nil {
		log.Printf("error running pruner: %s", err)
		os.Exit(1)
	}

	sort.Slice(result, func(i, j int) bool {
		return strings.Compare(result[i].Filesystem.ToString(), result[j].Filesystem.ToString()) == -1
	})

	var b bytes.Buffer
	for _, r := range result {
		fmt.Fprintf(&b, "%s\n", r.Filesystem.ToString())

		if testPrunePolicyArgs.showKept {
			fmt.Fprintf(&b, "\tkept:\n")
			for _, v := range r.Keep {
				fmt.Fprintf(&b, "\t- %s\n", v.Name)
			}
		}

		if testPrunePolicyArgs.showRemoved {
			fmt.Fprintf(&b, "\tremoved:\n")
			for _, v := range r.Remove {
				fmt.Fprintf(&b, "\t- %s\n", v.Name)
			}
		}

	}

	log.Printf("pruning result:\n%s", b.String())

}
