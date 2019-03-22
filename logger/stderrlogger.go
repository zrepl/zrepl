package logger

import (
	"fmt"
	"os"
)

type stderrLoggerOutlet struct{}

func (stderrLoggerOutlet) WriteEntry(entry Entry) error {
	fmt.Fprintf(os.Stderr, "%#v\n", entry)
	return nil
}

var _ Logger = testLogger{}

func NewStderrDebugLogger() Logger {
	outlets := NewOutlets()
	outlets.Add(&stderrLoggerOutlet{}, Debug)
	return &testLogger{
		Logger: NewLogger(outlets, 0),
	}
}
