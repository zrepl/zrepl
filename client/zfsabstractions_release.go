package client

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/pkg/errors"
	"github.com/spf13/pflag"

	"github.com/zrepl/zrepl/cli"
	"github.com/zrepl/zrepl/endpoint"
)

// shared between release-all and release-step
var zabsReleaseFlags struct {
	Filter zabsFilterFlags
	Json   bool
	DryRun bool
}

func registerZabsReleaseFlags(s *pflag.FlagSet) {
	zabsReleaseFlags.Filter.registerZabsFilterFlags(s, "release")
	s.BoolVar(&zabsReleaseFlags.Json, "json", false, "emit json instead of pretty-printed")
	s.BoolVar(&zabsReleaseFlags.DryRun, "dry-run", false, "do a dry-run")
}

var zabsCmdReleaseAll = &cli.Subcommand{
	Use:             "release-all",
	Run:             doZabsReleaseAll,
	NoRequireConfig: true,
	Short:           `(DANGEROUS) release ALL zrepl ZFS abstractions (mostly useful for uninstalling zrepl completely or for "de-zrepl-ing" a filesystem)`,
	SetupFlags:      registerZabsReleaseFlags,
}

var zabsCmdReleaseStale = &cli.Subcommand{
	Use:             "release-stale",
	Run:             doZabsReleaseStale,
	NoRequireConfig: true,
	Short:           `release stale zrepl ZFS abstractions (useful if zrepl has a bug and does not do it by itself)`,
	SetupFlags:      registerZabsReleaseFlags,
}

func doZabsReleaseAll(ctx context.Context, sc *cli.Subcommand, args []string) error {
	var err error

	if len(args) > 0 {
		return errors.New("this subcommand takes no positional arguments")
	}

	q, err := zabsReleaseFlags.Filter.Query()
	if err != nil {
		return errors.Wrap(err, "invalid filter specification on command line")
	}

	abstractions, listErrors, err := endpoint.ListAbstractions(ctx, q)
	if err != nil {
		return err // context clear by invocation of command
	}
	if len(listErrors) > 0 {
		color.New(color.FgRed).Fprintf(os.Stderr, "there were errors in listing the abstractions:\n%s\n", listErrors)
		// proceed anyways with rest of abstractions
	}

	return doZabsRelease_Common(ctx, abstractions)
}

func doZabsReleaseStale(ctx context.Context, sc *cli.Subcommand, args []string) error {

	var err error

	if len(args) > 0 {
		return errors.New("this subcommand takes no positional arguments")
	}

	q, err := zabsReleaseFlags.Filter.Query()
	if err != nil {
		return errors.Wrap(err, "invalid filter specification on command line")
	}

	stalenessInfo, err := endpoint.ListStale(ctx, q)
	if err != nil {
		return err // context clear by invocation of command
	}

	return doZabsRelease_Common(ctx, stalenessInfo.Stale)
}

func doZabsRelease_Common(ctx context.Context, destroy []endpoint.Abstraction) error {

	if zabsReleaseFlags.DryRun {
		if zabsReleaseFlags.Json {
			m, err := json.MarshalIndent(destroy, "", "  ")
			if err != nil {
				panic(err)
			}
			if _, err := os.Stdout.Write(m); err != nil {
				panic(err)
			}
			fmt.Println()
		} else {
			for _, a := range destroy {
				fmt.Printf("would destroy %s\n", a)
			}
		}
		return nil
	}

	outcome := endpoint.BatchDestroy(ctx, destroy)
	hadErr := false

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	colorErr := color.New(color.FgRed)
	printfSuccess := color.New(color.FgGreen).FprintfFunc()
	printfSection := color.New(color.Bold).FprintfFunc()

	for res := range outcome {
		hadErr = hadErr || res.DestroyErr != nil
		if zabsReleaseFlags.Json {
			err := enc.Encode(res)
			if err != nil {
				colorErr.Fprintf(os.Stderr, "cannot marshal there were errors in destroying the abstractions")
			}
		} else {
			printfSection(os.Stdout, "destroy %s ...", res.Abstraction)
			if res.DestroyErr != nil {
				colorErr.Fprintf(os.Stdout, " failed:\n%s\n", res.DestroyErr)
			} else {
				printfSuccess(os.Stdout, " OK\n")
			}
		}
	}

	if hadErr {
		colorErr.Add(color.Bold).Fprintf(os.Stderr, "there were errors in destroying the abstractions")
		return fmt.Errorf("")
	} else {
		return nil
	}
}
