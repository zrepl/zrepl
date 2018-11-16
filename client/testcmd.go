package client

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/spf13/pflag"
	"github.com/zrepl/zrepl/cli"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/filters"
	"github.com/zrepl/zrepl/zfs"
)

var TestCmd = &cli.Subcommand {
	Use: "test",
	SetupSubcommands: func() []*cli.Subcommand {
		return []*cli.Subcommand{testFilter, testPlaceholder}
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

var testPlaceholderArgs struct {
	action string
	ds     string
	plv    string
	all    bool
}

var testPlaceholder = &cli.Subcommand{
	Use:   "placeholder [--all | --dataset DATASET --action [compute | check [--placeholder PROP_VALUE]]]",
	Short: fmt.Sprintf("list received placeholder filesystems & compute the ZFS property %q", zfs.ZREPL_PLACEHOLDER_PROPERTY_NAME),
	Example: `
	placeholder --all
	placeholder --dataset path/to/sink/clientident/fs --action compute
	placeholder --dataset path/to/sink/clientident/fs --action check --placeholder 1671a61be44d32d1f3f047c5f124b06f98f54143d82900545ee529165060b859`,
	NoRequireConfig: true,
	SetupFlags: func(f *pflag.FlagSet) {
		f.StringVar(&testPlaceholderArgs.action, "action", "", "check | compute")
		f.StringVar(&testPlaceholderArgs.ds, "dataset", "", "dataset path (not required to exist)")
		f.StringVar(&testPlaceholderArgs.plv, "placeholder", "", "existing placeholder value to check against DATASET path")
		f.BoolVar(&testPlaceholderArgs.all, "all", false, "list tab-separated placeholder status of all filesystems")
	},
	Run: runTestPlaceholder,
}

func runTestPlaceholder(subcommand *cli.Subcommand, args []string) error {

	// all actions first
	if testPlaceholderArgs.all {
		out, err := zfs.ZFSList([]string{"name", zfs.ZREPL_PLACEHOLDER_PROPERTY_NAME})
		if err != nil {
			return errors.Wrap(err, "could not list ZFS filesystems")
		}
		fmt.Printf("IS_PLACEHOLDER\tDATASET\tPROPVALUE\tCOMPUTED\n")
		for _, row := range out {
			dp, err := zfs.NewDatasetPath(row[0])
			if err != nil {
				panic(err)
			}
			computedProp := zfs.PlaceholderPropertyValue(dp)
			is := "yes"
			if computedProp != row[1] {
				is = "no"
			}
			fmt.Printf("%s\t%s\t%q\t%q\n", is, dp.ToString(), row[1], computedProp)
		}
		return nil
	}

	// other actions

	dp, err := zfs.NewDatasetPath(testPlaceholderArgs.ds)
	if err != nil {
		return err
	}

	computedProp := zfs.PlaceholderPropertyValue(dp)

	switch testPlaceholderArgs.action {
	case "check":
		var isPlaceholder bool
		if testPlaceholderArgs.plv != "" {
			isPlaceholder = computedProp == testPlaceholderArgs.plv
		} else {
			isPlaceholder, err = zfs.ZFSIsPlaceholderFilesystem(dp)
			if err != nil {
				return err
			}
		}
		if isPlaceholder {
			fmt.Printf("%s is placeholder\n", dp.ToString())
			return nil
		} else {
			return fmt.Errorf("%s is not a placeholder", dp.ToString())
		}
	case "compute":
		fmt.Printf("%s\n", computedProp)
		return nil
	}

	return fmt.Errorf("unknown --action %q", testPlaceholderArgs.action)
}
