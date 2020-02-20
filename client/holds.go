package client

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/pflag"

	"github.com/zrepl/zrepl/cli"
	"github.com/zrepl/zrepl/daemon/filters"
	"github.com/zrepl/zrepl/endpoint"
	"github.com/zrepl/zrepl/zfs"
)

var (
	HoldsCmd = &cli.Subcommand{
		Use:   "holds",
		Short: "manage holds & step bookmarks",
		SetupSubcommands: func() []*cli.Subcommand {
			return holdsCmdList
		},
	}
)

var holdsCmdList = []*cli.Subcommand{
	&cli.Subcommand{
		Use:             "list [FSFILTER]",
		Run:             doHoldsList,
		NoRequireConfig: true,
		Short:           "list holds and bookmarks",
	},
	&cli.Subcommand{
		Use: "release [FSFILTER]",
		Run: doHoldsRelease,
		SetupFlags: func(f *pflag.FlagSet) {
			f.StringVar(&holdsReleaseFlags.Job, "job", "", "only release holds created by the specified job")
			//f.StringVar(&holdsReleaseFlags.Type, "type", "all", "only release holds of the specified type. [all|hold|bookmark]")
			f.BoolVar(&holdsReleaseFlags.All, "all", false, "release all holds instead of just duplicates")
			f.BoolVar(&holdsReleaseFlags.DryRun, "dry-run", false, "dry run")
		},
		NoRequireConfig: true,
		Short: `release duplicate or all holds, optionally filtered by job and/or FS

FSFILTER SYNTAX:
representation of a 'filesystems' filter statement on the command line
		`,
	},
}

var holdsReleaseFlags struct {
	All bool
	Job string
	//Type string
	DryRun bool
}

func fsfilterFromCliArg(arg string) (zfs.DatasetFilter, error) {
	mappings := strings.Split(arg, ",")
	f := filters.NewDatasetMapFilter(len(mappings), true)
	for _, m := range mappings {
		thisMappingErr := fmt.Errorf("expecting comma-separated list of <dataset-pattern>:<ok|!> pairs, got %q", m)
		lhsrhs := strings.SplitN(m, ":", 2)
		if len(lhsrhs) != 2 {
			return nil, thisMappingErr
		}
		err := f.Add(lhsrhs[0], lhsrhs[1])
		if err != nil {
			return nil, fmt.Errorf("%s: %s", thisMappingErr, err)
		}
	}
	return f.AsFilter(), nil
}

func doHoldsList(sc *cli.Subcommand, args []string) error {
	var err error
	ctx := context.Background()

	if len(args) > 1 {
		return errors.New("this subcommand takes at most one argument")
	}

	var filter zfs.DatasetFilter
	if len(args) == 0 {
		filter = zfs.NoFilter()
	} else {
		filter, err = fsfilterFromCliArg(args[0])
		if err != nil {
			return errors.Wrap(err, "cannot parse filesystem filter args")
		}
	}

	listing, err := endpoint.ListZFSHoldsAndBookmarks(ctx, filter)
	if err != nil {
		return err // context clear by invocation of command
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("  ", "  ")
	if err := enc.Encode(listing); err != nil {
		panic(err)
	}

	return nil
}

func doHoldsRelease(sc *cli.Subcommand, args []string) error {
	var err error
	ctx := context.Background()

	if len(args) > 1 {
		return errors.New("this subcommand takes at most one argument")
	}

	var filter zfs.DatasetFilter
	if len(args) == 0 {
		filter = zfs.NoFilter()
	} else {
		filter, err = fsfilterFromCliArg(args[0])
		if err != nil {
			return errors.Wrap(err, "cannot parse filesystem filter args")
		}
	}

	job, _ := endpoint.MakeJobID(holdsReleaseFlags.Job)

	keep := uint(1)
	if holdsReleaseFlags.All {
		keep = 0
	}
	return endpoint.ReleaseRedundantZFSHoldsAndBookmarks(ctx, filter, job, keep, holdsReleaseFlags.DryRun)
}
