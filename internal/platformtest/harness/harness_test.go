package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zrepl/zrepl/internal/platformtest"
)

// Idea taken from
// https://github.com/openSUSE/umoci/blob/v0.2.1/cmd/umoci/main_test.go
//
/* How to generate coverage:
   go test -c -covermode=atomic -cover -coverpkg github.com/zrepl/zrepl/...
   ./harness.test -test.coverprofile=/tmp/harness.out \
       -test.v __DEVEL--i-heard-you-like-tests
    go tool cover -html=/tmp/harness.out -o /tmp/harness.html
*/
// Merge with existing coverage reports using gocovmerge:
//  https://github.com/wadey/gocovmerge

func TestMain(t *testing.T) {
	// Multi-personality: when invoked as zfs/zpool symlink, run interposer
	switch filepath.Base(os.Args[0]) {
	case "zfs":
		os.Exit(platformtest.RunInterposer("zfs"))
	case "zpool":
		os.Exit(platformtest.RunInterposer("zpool"))
	}

	fmt.Println("incoming args: ", os.Args)

	var (
		args             []string
		run              bool
		startCaptureArgs bool
	)

	for i, arg := range os.Args {
		switch {
		case arg == "__DEVEL--i-heard-you-like-tests":
			run = true
			startCaptureArgs = true
		case strings.HasPrefix(arg, "-test"):
		case strings.HasPrefix(arg, "__DEVEL"):
		case i == 0:
			args = append(args, arg)
		case startCaptureArgs:
			args = append(args, arg)
		}
	}
	os.Args = args
	fmt.Println("using args: ", os.Args)

	if run {
		main()
	}
}
