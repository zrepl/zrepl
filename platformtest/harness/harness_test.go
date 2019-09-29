package main

import (
	"os"
	"strings"
	"testing"
)

// Idea taken from
// https://github.com/openSUSE/umoci/blob/v0.2.1/cmd/umoci/main_test.go
//
/* How to generate coverage:
   go test -c -covermode=atomic -cover -coverpkg github.com/zrepl/zrepl/...
   sudo ../logmockzfs/logzfsenv /tmp/zrepl_platform_test.log /usr/bin/zfs \
       ./harness.test -test.coverprofile=/tmp/harness.out \
       -test.v __DEVEL--i-heard-you-like-tests \
       -imagepath /tmp/testpool.img -poolname zreplplatformtest
    go tool cover -html=/tmp/harness.out -o /tmp/harness.html
*/
// Merge with existing coverage reports using gocovmerge:
//  https://github.com/wadey/gocovmerge

func TestMain(t *testing.T) {
	var (
		args []string
		run  bool
	)

	for _, arg := range os.Args {
		switch {
		case arg == "__DEVEL--i-heard-you-like-tests":
			run = true
		case strings.HasPrefix(arg, "-test"):
		case strings.HasPrefix(arg, "__DEVEL"):
		default:
			args = append(args, arg)
		}
	}
	os.Args = args

	if run {
		main()
	}
}
