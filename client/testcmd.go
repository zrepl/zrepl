package client

import (
	"fmt"
	"github.com/spf13/pflag"
	"github.com/zrepl/zrepl/cli"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/filters"
	"github.com/zrepl/zrepl/zfs"
)

var TestCmd = &cli.Subcommand {
	Use: "test",
	SetupSubcommands: func() []*cli.Subcommand {
		return []*cli.Subcommand{testFilter}
	},
}

var testFilterArgs struct {
	job string
	all bool
	input string
}

var testFilter = &cli.Subcommand{
	Use: "filesystems --job JOB [--all | --input INPUT]",
	Short: "test filesystems filter specified in push or source job",
	SetupFlags: func(f *pflag.FlagSet) {
		f.StringVar(&testFilterArgs.job, "job", "", "the name of the push or source job")
		f.StringVar(&testFilterArgs.input, "input", "", "a filesystem name to test against the job's filters")
		f.BoolVar(&testFilterArgs.all, "all", false, "test all local filesystems")
	},
	Run: runTestFilterCmd,
}

func runTestFilterCmd(subcommand *cli.Subcommand, args []string) error {

	if testFilterArgs.job == "" {
		return fmt.Errorf("must specify --job flag")
	}
	if !(testFilterArgs.all != (testFilterArgs.input != "")) { // xor
		return fmt.Errorf("must set one: --all or --input")
	}

	conf := subcommand.Config()

	var confFilter config.FilesystemsFilter
	job, err := conf.Job(testFilterArgs.job)
	if err != nil {
		return err
	}
	switch j := job.Ret.(type) {
	case *config.SourceJob: confFilter = j.Filesystems
	case *config.PushJob: confFilter = j.Filesystems
	default:
		return fmt.Errorf("job type %T does not have filesystems filter", j)
	}

	f, err := filters.DatasetMapFilterFromConfig(confFilter)
	if err != nil {
		return fmt.Errorf("filter invalid: %s", err)
	}

	var fsnames []string
	if testFilterArgs.input != "" {
		fsnames = []string{testFilterArgs.input}
	} else {
		out, err := zfs.ZFSList([]string{"name"})
		if err != nil {
			return fmt.Errorf("could not list ZFS filesystems: %s", err)
		}
		for _, row := range out {

			fsnames = append(fsnames, row[0])
		}
	}

	fspaths := make([]*zfs.DatasetPath, len(fsnames))
	for i, fsname := range fsnames {
		path, err := zfs.NewDatasetPath(fsname)
		if err != nil {
			return err
		}
		fspaths[i] = path
	}

	hadFilterErr := false
	for _, in := range fspaths {
		var res string
		var errStr string
		pass, err := f.Filter(in)
		if err != nil {
			res = "ERROR"
			errStr = err.Error()
			hadFilterErr = true
		} else if pass {
			res = "ACCEPT"
		} else {
			res = "REJECT"
		}
		fmt.Printf("%s\t%s\t%s\n", res, in.ToString(), errStr)
	}

	if hadFilterErr {
		return fmt.Errorf("filter errors occurred")
	}
	return nil
}