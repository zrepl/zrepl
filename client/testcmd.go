package client

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/pkg/errors"
	"github.com/spf13/pflag"

	"github.com/zrepl/zrepl/cli"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/filters"
	"github.com/zrepl/zrepl/zfs"
)

var TestCmd = &cli.Subcommand{
	Use: "test",
	SetupSubcommands: func() []*cli.Subcommand {
		return []*cli.Subcommand{testFilter, testPlaceholder, testDecodeResumeToken}
	},
}

var testFilterArgs struct {
	job   string
	all   bool
	input string
}

var testFilter = &cli.Subcommand{
	Use:   "filesystems --job JOB [--all | --input INPUT]",
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
	case *config.SourceJob:
		confFilter = j.Filesystems
	case *config.PushJob:
		confFilter = j.Filesystems
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
	ds  string
	all bool
}

var testPlaceholder = &cli.Subcommand{
	Use:   "placeholder [--all | --dataset DATASET]",
	Short: fmt.Sprintf("list received placeholder filesystems (zfs property %q)", zfs.PlaceholderPropertyName),
	Example: `
	placeholder --all
	placeholder --dataset path/to/sink/clientident/fs`,
	NoRequireConfig: true,
	SetupFlags: func(f *pflag.FlagSet) {
		f.StringVar(&testPlaceholderArgs.ds, "dataset", "", "dataset path (not required to exist)")
		f.BoolVar(&testPlaceholderArgs.all, "all", false, "list tab-separated placeholder status of all filesystems")
	},
	Run: runTestPlaceholder,
}

func runTestPlaceholder(subcommand *cli.Subcommand, args []string) error {

	var checkDPs []*zfs.DatasetPath

	// all actions first
	if testPlaceholderArgs.all {
		out, err := zfs.ZFSList([]string{"name"})
		if err != nil {
			return errors.Wrap(err, "could not list ZFS filesystems")
		}
		for _, row := range out {
			dp, err := zfs.NewDatasetPath(row[0])
			if err != nil {
				panic(err)
			}
			checkDPs = append(checkDPs, dp)
		}
	} else {
		dp, err := zfs.NewDatasetPath(testPlaceholderArgs.ds)
		if err != nil {
			return err
		}
		if dp.Empty() {
			return fmt.Errorf("must specify --dataset DATASET or --all")
		}
		checkDPs = append(checkDPs, dp)
	}

	fmt.Printf("IS_PLACEHOLDER\tDATASET\tzrepl:placeholder\n")
	for _, dp := range checkDPs {
		ph, err := zfs.ZFSGetFilesystemPlaceholderState(dp)
		if err != nil {
			return errors.Wrap(err, "cannot get placeholder state")
		}
		if !ph.FSExists {
			panic("placeholder state inconsistent: filesystem " + ph.FS + " must exist in this context")
		}
		is := "yes"
		if !ph.IsPlaceholder {
			is = "no"
		}
		fmt.Printf("%s\t%s\t%s\n", is, dp.ToString(), ph.RawLocalPropertyValue)
	}
	return nil
}

var testDecodeResumeTokenArgs struct {
	token string
}

var testDecodeResumeToken = &cli.Subcommand{
	Use:   "decoderesumetoken --token TOKEN",
	Short: "decode resume token",
	SetupFlags: func(f *pflag.FlagSet) {
		f.StringVar(&testDecodeResumeTokenArgs.token, "token", "", "the resume token obtained from the receive_resume_token property")
	},
	Run: runTestDecodeResumeTokenCmd,
}

func runTestDecodeResumeTokenCmd(subcommand *cli.Subcommand, args []string) error {
	if testDecodeResumeTokenArgs.token == "" {
		return fmt.Errorf("token argument must be specified")
	}
	token, err := zfs.ParseResumeToken(context.Background(), testDecodeResumeTokenArgs.token)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", " ")
	if err := enc.Encode(&token); err != nil {
		panic(err)
	}
	return nil
}
