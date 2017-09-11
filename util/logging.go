package util

import "fmt"

type Logger interface {
	Printf(format string, args ...interface{})
}

type PrefixLogger struct {
	Log    Logger
	Prefix string
}

func NewPrefixLogger(logger Logger, prefix string) (l PrefixLogger) {
	return PrefixLogger{logger, prefix}
}

func (l PrefixLogger) Printf(format string, v ...interface{}) {
	l.Log.Printf(fmt.Sprintf("[%s]: %s", l.Prefix, format), v...)
}
