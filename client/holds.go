package client

import (
	"fmt"
	"sort"
	"strings"

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
			return []*cli.Subcommand{
				holdsCmdList,
				holdsCmdReleaseAll,
				holdsCmdReleaseStale,
			}
		},
	}
)

// a common set of CLI flags that map to the fields of an
// endpoint.ListZFSHoldsAndBookmarksQuery
type holdsFilterFlags struct {
	Filesystems FilesystemsFilterFlag
	Job         JobIDFlag
	Types       AbstractionTypesFlag
	Concurrency int64
}

// produce a query from the CLI flags
func (f holdsFilterFlags) Query() (endpoint.ListZFSHoldsAndBookmarksQuery, error) {
	q := endpoint.ListZFSHoldsAndBookmarksQuery{
		FS:          f.Filesystems.FlagValue(),
		What:        f.Types.FlagValue(),
		JobID:       f.Job.FlagValue(),
		Concurrency: f.Concurrency,
	}
	return q, q.Validate()
}

func (f *holdsFilterFlags) registerHoldsFilterFlags(s *pflag.FlagSet, verb string) {
	// Note: the default value is defined in the .FlagValue methods
	s.Var(&f.Filesystems, "fs", fmt.Sprintf("only %s holds on the specified filesystem [default: all filesystems] [comma-separated list of <dataset-pattern>:<ok|!> pairs]", verb))
	s.Var(&f.Job, "job", fmt.Sprintf("only %s holds created by the specified job [default: any job]", verb))

	variants := make([]string, 0, len(endpoint.AbstractionTypesAll))
	for v := range endpoint.AbstractionTypesAll {
		variants = append(variants, string(v))
	}
	variants = sort.StringSlice(variants)
	variantsJoined := strings.Join(variants, "|")
	s.Var(&f.Types, "type", fmt.Sprintf("only %s holds of the specified type [default: all] [comma-separated list of %s]", verb, variantsJoined))

	s.Int64VarP(&f.Concurrency, "concurrency", "p", 1, "number of concurrently queried filesystems")
}

type JobIDFlag struct{ J *endpoint.JobID }

func (f *JobIDFlag) Set(s string) error {
	if len(s) == 0 {
		*f = JobIDFlag{J: nil}
		return nil
	}

	jobID, err := endpoint.MakeJobID(s)
	if err != nil {
		return err
	}
	*f = JobIDFlag{J: &jobID}
	return nil
}
func (f JobIDFlag) Type() string               { return "job-ID" }
func (f JobIDFlag) String() string             { return fmt.Sprint(f.J) }
func (f JobIDFlag) FlagValue() *endpoint.JobID { return f.J }

type AbstractionTypesFlag map[endpoint.AbstractionType]bool

func (f *AbstractionTypesFlag) Set(s string) error {
	ats, err := endpoint.AbstractionTypeSetFromStrings(strings.Split(s, ","))
	if err != nil {
		return err
	}
	*f = AbstractionTypesFlag(ats)
	return nil
}
func (f AbstractionTypesFlag) Type() string { return "abstraction-type" }
func (f AbstractionTypesFlag) String() string {
	return endpoint.AbstractionTypeSet(f).String()
}
func (f AbstractionTypesFlag) FlagValue() map[endpoint.AbstractionType]bool {
	if len(f) > 0 {
		return f
	}
	return endpoint.AbstractionTypesAll
}

type FilesystemsFilterFlag struct {
	F endpoint.ListZFSHoldsAndBookmarksQueryFilesystemFilter
}

func (flag *FilesystemsFilterFlag) Set(s string) error {
	mappings := strings.Split(s, ",")
	if len(mappings) == 1 && !strings.Contains(mappings[0], ":") {
		flag.F = endpoint.ListZFSHoldsAndBookmarksQueryFilesystemFilter{
			FS: &mappings[0],
		}
		return nil
	}

	f := filters.NewDatasetMapFilter(len(mappings), true)
	for _, m := range mappings {
		thisMappingErr := fmt.Errorf("expecting comma-separated list of <dataset-pattern>:<ok|!> pairs, got %q", m)
		lhsrhs := strings.SplitN(m, ":", 2)
		if len(lhsrhs) != 2 {
			return thisMappingErr
		}
		err := f.Add(lhsrhs[0], lhsrhs[1])
		if err != nil {
			return fmt.Errorf("%s: %s", thisMappingErr, err)
		}
	}
	flag.F = endpoint.ListZFSHoldsAndBookmarksQueryFilesystemFilter{
		Filter: f,
	}
	return nil
}
func (flag FilesystemsFilterFlag) Type() string { return "filesystem filter spec" }
func (flag FilesystemsFilterFlag) String() string {
	return fmt.Sprintf("%v", flag.F)
}
func (flag FilesystemsFilterFlag) FlagValue() endpoint.ListZFSHoldsAndBookmarksQueryFilesystemFilter {
	var z FilesystemsFilterFlag
	if flag == z {
		return endpoint.ListZFSHoldsAndBookmarksQueryFilesystemFilter{Filter: zfs.NoFilter()}
	}
	return flag.F
}
