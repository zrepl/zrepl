package logger

import (
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"sync"
	"time"
)

type Level int

func (l Level) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

func (l *Level) UnmarshalJSON(input []byte) (err error) {
	var s string
	if err = json.Unmarshal(input, &s); err != nil {
		return err
	}
	*l, err = ParseLevel(s)
	return err
}

const (
	Debug Level = iota
	Info
	Warn
	Error
)

func (l Level) Short() string {
	switch l {
	case Debug:
		return "DEBG"
	case Info:
		return "INFO"
	case Warn:
		return "WARN"
	case Error:
		return "ERRO"
	default:
		return fmt.Sprintf("%s", l)
	}
}

func (l Level) String() string {
	switch l {
	case Debug:
		return "debug"
	case Info:
		return "info"
	case Warn:
		return "warn"
	case Error:
		return "error"
	default:
		return fmt.Sprintf("%s", string(l))
	}
}

func ParseLevel(s string) (l Level, err error) {
	for _, l := range AllLevels {
		if s == l.String() {
			return l, nil
		}
	}
	return -1, errors.Errorf("unknown level '%s'", s)
}

// Levels ordered least severe to most severe
var AllLevels []Level = []Level{Debug, Info, Warn, Error}

type Fields map[string]interface{}

type Entry struct {
	Level   Level
	Message string
	Time    time.Time
	Fields  Fields
}

// An outlet receives log entries produced by the Logger and writes them to some destination.
type Outlet interface {
	// Write the entry to the destination.
	//
	// Logger waits for all outlets to return from WriteEntry() before returning from the log call.
	// An implementation of Outlet must assert that it does not block in WriteEntry.
	// Otherwise, it will slow down the program.
	//
	// Note: os.Stderr is also used by logger.Logger for reporting errors returned by outlets
	//       => you probably don't want to log there
	WriteEntry(entry Entry) error
}

type Outlets struct {
	mtx  sync.RWMutex
	outs map[Level][]Outlet
}

func NewOutlets() *Outlets {
	return &Outlets{
		mtx:  sync.RWMutex{},
		outs: make(map[Level][]Outlet, len(AllLevels)),
	}
}

func (os *Outlets) DeepCopy() (copy *Outlets) {
	copy = NewOutlets()
	for level := range os.outs {
		for i := range os.outs[level] {
			copy.outs[level] = append(copy.outs[level], os.outs[level][i])
		}
	}
	return copy
}

func (os *Outlets) Add(outlet Outlet, minLevel Level) {
	os.mtx.Lock()
	defer os.mtx.Unlock()
	for _, l := range AllLevels[minLevel:] {
		os.outs[l] = append(os.outs[l], outlet)
	}
}

func (os *Outlets) Get(level Level) []Outlet {
	os.mtx.RLock()
	defer os.mtx.RUnlock()
	return os.outs[level]
}

// Return the first outlet added to this Outlets list using Add()
// with minLevel <= Error.
// If no such outlet is in this Outlets list, a discarding outlet is returned.
func (os *Outlets) GetLoggerErrorOutlet() Outlet {
	os.mtx.RLock()
	defer os.mtx.RUnlock()
	if len(os.outs[Error]) < 1 {
		return nullOutlet{}
	}
	return os.outs[Error][0]
}

type nullOutlet struct{}

func (nullOutlet) WriteEntry(entry Entry) error { return nil }
