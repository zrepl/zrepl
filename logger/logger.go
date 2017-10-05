package logger

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"sync"
	"time"
)

const (
	// The field set by WithError function
	FieldError = "err"
)

const DefaultUserFieldCapacity = 5
const InternalErrorPrefix = "github.com/zrepl/zrepl/logger: "

type Logger struct {
	fields        Fields
	outlets       Outlets
	outletTimeout time.Duration

	mtx *sync.Mutex
}

func NewLogger(outlets Outlets, outletTimeout time.Duration) *Logger {
	return &Logger{
		make(Fields, DefaultUserFieldCapacity),
		outlets,
		outletTimeout,
		&sync.Mutex{},
	}
}

func (l *Logger) log(level Level, msg string) {

	l.mtx.Lock()
	defer l.mtx.Unlock()

	entry := Entry{level, msg, time.Now(), l.fields}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(l.outletTimeout))
	ech := make(chan error)

	louts := l.outlets[level]
	for i := range louts {
		go func(ctx context.Context, outlet Outlet, entry Entry) {
			ech <- outlet.WriteEntry(ctx, entry)
		}(ctx, louts[i], entry)
	}

	for fin := 0; fin < len(louts); fin++ {
		select {
		case err := <-ech:
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s outlet error: %s\n", InternalErrorPrefix, err)
			}
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				fmt.Fprintf(os.Stderr, "%s outlets exceeded deadline, keep waiting anyways", InternalErrorPrefix)
			}
		}
	}

	cancel() // make go vet happy

}

func (l *Logger) WithField(field string, val interface{}) *Logger {

	l.mtx.Lock()
	defer l.mtx.Unlock()

	if _, ok := l.fields[field]; ok {
		fmt.Fprintf(os.Stderr, "%s caller overwrites field '%s'. Stack:\n%s\n", InternalErrorPrefix, field, string(debug.Stack()))
	}

	child := &Logger{
		fields:        make(Fields, len(l.fields)+1),
		outlets:       l.outlets, // cannot be changed after logger initialized
		outletTimeout: l.outletTimeout,
		mtx:           l.mtx,
	}
	for k, v := range l.fields {
		child.fields[k] = v
	}
	child.fields[field] = val

	return child

}

func (l *Logger) WithFields(fields Fields) (ret *Logger) {
	// TODO optimize
	ret = l
	for field, value := range fields {
		ret = l.WithField(field, value)
	}
	return ret
}

func (l *Logger) WithError(err error) *Logger {
	val := interface{}(nil)
	if err != nil {
		val = err.Error()
	}
	return l.WithField(FieldError, val)
}

func (l *Logger) Debug(msg string) {
	l.log(Debug, msg)
}

func (l *Logger) Info(msg string) {
	l.log(Info, msg)
}

func (l *Logger) Warn(msg string) {
	l.log(Warn, msg)
}

func (l *Logger) Error(msg string) {
	l.log(Error, msg)
}

func (l *Logger) Printf(format string, args ...interface{}) {
	l.log(Error, fmt.Sprintf(format, args...))
}
