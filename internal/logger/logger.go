package logger

import (
	"fmt"
	"runtime/debug"
	"sync"
	"time"
)

const (
	// The field set by WithError function
	FieldError = "err"
)

const DefaultUserFieldCapacity = 5

type Logger interface {
	WithOutlet(outlet Outlet, level Level) Logger
	ReplaceField(field string, val interface{}) Logger
	WithField(field string, val interface{}) Logger
	WithFields(fields Fields) Logger
	WithError(err error) Logger
	Log(level Level, msg string)
	Debug(msg string)
	Info(msg string)
	Warn(msg string)
	Error(msg string)
	Printf(format string, args ...interface{})
}

type loggerImpl struct {
	fields        Fields
	outlets       *Outlets
	outletTimeout time.Duration

	mtx *sync.Mutex
}

var _ Logger = &loggerImpl{}

func NewLogger(outlets *Outlets, outletTimeout time.Duration) Logger {
	return &loggerImpl{
		make(Fields, DefaultUserFieldCapacity),
		outlets,
		outletTimeout,
		&sync.Mutex{},
	}
}

type outletResult struct {
	Outlet Outlet
	Error  error
}

func (l *loggerImpl) logInternalError(outlet Outlet, err string) {
	fields := Fields{}
	if outlet != nil {
		if _, ok := outlet.(fmt.Stringer); ok {
			fields["outlet"] = fmt.Sprintf("%s", outlet)
		}
		fields["outlet_type"] = fmt.Sprintf("%T", outlet)
	}
	fields[FieldError] = err
	entry := Entry{
		Error,
		"outlet error",
		time.Now(),
		fields,
	}
	// ignore errors at this point (still better than panicking if the error is temporary)
	_ = l.outlets.GetLoggerErrorOutlet().WriteEntry(entry)
}

func (l *loggerImpl) log(level Level, msg string) {

	l.mtx.Lock()
	defer l.mtx.Unlock()

	entry := Entry{level, msg, time.Now(), l.fields}

	louts := l.outlets.Get(level)
	ech := make(chan outletResult, len(louts))
	for i := range louts {
		go func(outlet Outlet, entry Entry) {
			ech <- outletResult{outlet, outlet.WriteEntry(entry)}
		}(louts[i], entry)
	}
	for fin := 0; fin < len(louts); fin++ {
		res := <-ech
		if res.Error != nil {
			l.logInternalError(res.Outlet, res.Error.Error())
		}
	}
	close(ech)

}

func (l *loggerImpl) WithOutlet(outlet Outlet, level Level) Logger {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	newOutlets := l.outlets.DeepCopy()
	newOutlets.Add(outlet, level)
	child := &loggerImpl{
		fields:        l.fields,
		outlets:       newOutlets,
		outletTimeout: l.outletTimeout,
		mtx:           l.mtx,
	}
	return child
}

// callers must hold l.mtx
func (l *loggerImpl) forkLogger(field string, val interface{}) *loggerImpl {

	child := &loggerImpl{
		fields:        make(Fields, len(l.fields)+1),
		outlets:       l.outlets,
		outletTimeout: l.outletTimeout,
		mtx:           l.mtx,
	}
	for k, v := range l.fields {
		child.fields[k] = v
	}
	child.fields[field] = val

	return child
}

func (l *loggerImpl) ReplaceField(field string, val interface{}) Logger {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	return l.forkLogger(field, val)
}

func (l *loggerImpl) WithField(field string, val interface{}) Logger {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	if val, ok := l.fields[field]; ok && val != nil {
		l.logInternalError(nil,
			fmt.Sprintf("caller overwrites field '%s'. Stack: %s", field, string(debug.Stack())))
	}
	return l.forkLogger(field, val)
}

func (l *loggerImpl) WithFields(fields Fields) Logger {
	// TODO optimize
	var ret Logger = l
	for field, value := range fields {
		ret = ret.WithField(field, value)
	}
	return ret
}

func (l *loggerImpl) WithError(err error) Logger {
	val := interface{}(nil)
	if err != nil {
		val = err.Error()
	}
	return l.WithField(FieldError, val)
}

func (l *loggerImpl) Log(level Level, msg string) {
	l.log(level, msg)
}

func (l *loggerImpl) Debug(msg string) {
	l.log(Debug, msg)
}

func (l *loggerImpl) Info(msg string) {
	l.log(Info, msg)
}

func (l *loggerImpl) Warn(msg string) {
	l.log(Warn, msg)
}

func (l *loggerImpl) Error(msg string) {
	l.log(Error, msg)
}

func (l *loggerImpl) Printf(format string, args ...interface{}) {
	l.log(Error, fmt.Sprintf(format, args...))
}
