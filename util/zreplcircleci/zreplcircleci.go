package zreplcircleci

import (
	"fmt"
	"os"
	"testing"
)

func SkipOnCircleCI(t *testing.T, reasonFmt string, args ...interface{}) {
	if os.Getenv("CIRCLECI") != "" {
		t.Skipf("This test is skipped in CircleCI. Reason: %s", fmt.Sprintf(reasonFmt, args...))
	}
}
