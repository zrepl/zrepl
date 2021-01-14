package sshdirect

import (
	"fmt"
	"net"
	"os"
	"syscall"
)

type timeouter interface {
	Timeout() bool
}

var _ timeouter = &os.PathError{}

type IOError struct {
	Cause error
}

var _ net.Error = &IOError{}

func (e IOError) GoString() string {
	return fmt.Sprintf("ServeConnIOError:%#v", e.Cause)
}

func (e IOError) Error() string {
	// following case found by experiment
	if pathErr, ok := e.Cause.(*os.PathError); ok {
		if pathErr.Err == syscall.EPIPE {
			return fmt.Sprintf("netssh %s: %s (likely: connection reset by peer)",
				pathErr.Op, pathErr.Err,
			)
		}
		return fmt.Sprintf("netssh: %s: %s", pathErr.Op, pathErr.Err)
	}
	return fmt.Sprintf("netssh: %s", e.Cause.Error())
}

func (e IOError) Timeout() bool {
	if to, ok := e.Cause.(timeouter); ok {
		return to.Timeout()
	}
	return false
}

func (e IOError) Temporary() bool {
	return false
}
