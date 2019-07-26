package logger

type nullLogger struct{}

var _ Logger = nullLogger{}

func NewNullLogger() Logger {
	return nullLogger{}
}

func (n nullLogger) WithOutlet(outlet Outlet, level Level) Logger      { return n }
func (n nullLogger) ReplaceField(field string, val interface{}) Logger { return n }
func (n nullLogger) WithField(field string, val interface{}) Logger    { return n }
func (n nullLogger) WithFields(fields Fields) Logger                   { return n }
func (n nullLogger) WithError(err error) Logger                        { return n }
func (nullLogger) Log(level Level, msg string)                         {}
func (nullLogger) Debug(msg string)                                    {}
func (nullLogger) Info(msg string)                                     {}
func (nullLogger) Warn(msg string)                                     {}
func (nullLogger) Error(msg string)                                    {}
func (nullLogger) Printf(format string, args ...interface{})           {}
