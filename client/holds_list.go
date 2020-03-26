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

var holdsListFlags struct {
	Filter holdsFilterFlags
	Json   bool
}

var holdsCmdList = &cli.Subcommand{
	Use:             "list",
	Run:             doHoldsList,
	NoRequireConfig: true,
	Short:           "list holds and bookmarks",
	SetupFlags: func(f *pflag.FlagSet) {
		holdsListFlags.Filter.registerHoldsFilterFlags(f, "list")
		f.BoolVar(&holdsListFlags.Json, "json", false, "emit JSON")
	},
}

func doHoldsList(sc *cli.Subcommand, args []string) error {
	var err error
	ctx := context.Background()

	if len(args) > 0 {
		return errors.New("this subcommand takes no positional arguments")
	}

	q, err := holdsListFlags.Filter.Query()
	if err != nil {
		return errors.Wrap(err, "invalid filter specification on command line")
	}

	abstractions, errors, err := endpoint.ListAbstractions(ctx, q)
	if err != nil {
		return err // context clear by invocation of command
	}

	// always print what we got
	if holdsListFlags.Json {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(abstractions); err != nil {
			panic(err)
		}
		fmt.Println()
	} else {
		for _, a := range abstractions {
			fmt.Println(a)
		}
	}

	// then potential errors, so that users always see them
	if len(errors) > 0 {
		color.New(color.FgRed).Fprintf(os.Stderr, "there were errors in listing the abstractions:\n%s\n", errors)
		return fmt.Errorf("")
	} else {
		return nil
	}
}
