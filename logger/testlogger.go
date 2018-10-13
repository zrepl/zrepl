package logger

import (
	"testing"
)

type testLogger struct {
	Logger
}

type testingLoggerOutlet struct {
	t *testing.T
}

func (o testingLoggerOutlet) WriteEntry(entry Entry) error {
	o.t.Logf("%#v", entry)
	return nil
}

var _ Logger = testLogger{}

func NewTestLogger(t *testing.T) Logger {
	outlets := NewOutlets()
	outlets.Add(&testingLoggerOutlet{t}, Debug)
	return &testLogger{
		Logger: NewLogger(outlets, 0),
	}
}
