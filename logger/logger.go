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

type Logger struct {
	fields        Fields
	outlets       *Outlets
	outletTimeout time.Duration

	mtx *sync.Mutex
}

func NewLogger(outlets *Outlets, outletTimeout time.Duration) *Logger {
	return &Logger{
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

func (l *Logger) logInternalError(outlet Outlet, err string) {
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
	l.outlets.GetLoggerErrorOutlet().WriteEntry(entry)
}

func (l *Logger) log(level Level, msg string) {

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

func (l *Logger) WithOutlet(outlet Outlet, level Level) *Logger {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	newOutlets := l.outlets.DeepCopy()
	newOutlets.Add(outlet, level)
	child := &Logger{
		fields:        l.fields,
		outlets:       newOutlets,
		outletTimeout: l.outletTimeout,
		mtx:           l.mtx,
	}
	return child
}

func (l *Logger) ReplaceField(field string, val interface{}) *Logger {
	l.fields[field] = nil
	return l.WithField(field, val)
}

func (l *Logger) WithField(field string, val interface{}) *Logger {

	l.mtx.Lock()
	defer l.mtx.Unlock()

	if val, ok := l.fields[field]; ok && val != nil {
		l.logInternalError(nil,
			fmt.Sprintf("caller overwrites field '%s'. Stack: %s", field, string(debug.Stack())))
	}

	child := &Logger{
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
