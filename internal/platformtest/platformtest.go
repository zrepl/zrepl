package platformtest

import (
	"context"
	"fmt"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type Context struct {
	context.Context
	RootDataset string
	// Use this callback from a top-level test case to queue the
	// execution of sub-tests after this test case is complete.
	//
	// Note that the testing harness executes the subtest
	// _after_ the current top-level test. Hence, the subtest
	// cannot use any ZFS state of the top-level test.
	QueueSubtest func(id string, stf func(*Context))
}

var FailNowSentinel = fmt.Errorf("platformtest: FailNow called on context")

var SkipNowSentinel = fmt.Errorf("platformtest: SkipNow called on context")

var _ assert.TestingT = (*Context)(nil)
var _ require.TestingT = (*Context)(nil)

func (c *Context) Logf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	GetLog(c).Info(msg)
}

func (c *Context) Errorf(format string, args ...interface{}) {
	GetLog(c).Printf(format, args...)
	c.FailNow()
}

func (c *Context) FailNow() {
	panic(FailNowSentinel)
}

func (c *Context) SkipNow() {
	panic(SkipNowSentinel)
}
