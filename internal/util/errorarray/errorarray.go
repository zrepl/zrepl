package errorarray

import (
	"fmt"
	"strings"
)

type Errors struct {
	Msg     string
	Wrapped []error
}

var _ error = (*Errors)(nil)

func Wrap(errs []error, msg string) Errors {
	if len(errs) == 0 {
		panic("passing empty errs argument")
	}
	return Errors{Msg: msg, Wrapped: errs}
}

func (e Errors) Unwrap() error {
	if len(e.Wrapped) == 1 {
		return e.Wrapped[0]
	}
	return nil // ... limitation of the Go 1.13 errors API
}

func (e Errors) Error() string {
	if len(e.Wrapped) == 1 {
		return fmt.Sprintf("%s: %s", e.Msg, e.Wrapped[0])
	}
	var buf strings.Builder
	fmt.Fprintf(&buf, "%s: multiple errors:\n", e.Msg)
	for i, err := range e.Wrapped {
		fmt.Fprintf(&buf, "%s", err)
		if i != len(e.Wrapped)-1 {
			fmt.Fprintf(&buf, "\n")
		}
	}
	return buf.String()

}
