package cmd

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/zrepl/zrepl/zfs"
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "test configuration",
}

var testConfigSyntaxCmd = &cobra.Command{
	Use:   "config",
	Short: "test if config file can be parsed",
	Run:   doTestConfig,
}

var testDatasetMapFilter = &cobra.Command{
	Use:     "pattern jobtype.name test/zfs/dataset/path",
	Short:   "test dataset mapping / filter specified in config",
	Example: ` zrepl test pattern prune.clean_backups tank/backups/legacyscript/foo`,
	Run:     doTestDatasetMapFilter,
}

func init() {
	RootCmd.AddCommand(testCmd)
	testCmd.AddCommand(testConfigSyntaxCmd)
	testCmd.AddCommand(testDatasetMapFilter)
}

func doTestConfig(cmd *cobra.Command, args []string) {
	log.Printf("config ok")
	return
}

func doTestDatasetMapFilter(cmd *cobra.Command, args []string) {
	if len(args) != 2 {
		log.Printf("specify job name as first postitional argument, test input as second")
		log.Printf(cmd.UsageString())
		os.Exit(1)
	}
	n, i := args[0], args[1]
	jobi, err := conf.resolveJobName(n)
	if err != nil {
		log.Printf("%s", err)
		os.Exit(1)
	}

	var mf DatasetMapFilter
	switch j := jobi.(type) {
	case *Autosnap:
		mf = j.DatasetFilter
	case *Prune:
		mf = j.DatasetFilter
	case *Pull:
		mf = j.Mapping
	case *Push:
		mf = j.Filter
	case DatasetMapFilter:
		mf = j
	default:
		panic("incomplete implementation")
	}

	ip, err := zfs.NewDatasetPath(i)
	if err != nil {
		log.Printf("cannot parse test input as ZFS dataset path: %s", err)
		os.Exit(1)
	}

	if mf.filterOnly {
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
