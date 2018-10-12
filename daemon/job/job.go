package job

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/zrepl/zrepl/logger"
)

type Logger = logger.Logger

type contextKey int

const (
	contextKeyLog contextKey = iota
)

func GetLogger(ctx context.Context) Logger {
	if l, ok := ctx.Value(contextKeyLog).(Logger); ok {
		return l
	}
	return logger.NewNullLogger()
}

func WithLogger(ctx context.Context, l Logger) context.Context {
	return context.WithValue(ctx, contextKeyLog, l)
}


type Job interface {
	Name() string
	Run(ctx context.Context)
	Status() *Status
	RegisterMetrics(registerer prometheus.Registerer)
}

type Type string

const (
	TypeInternal Type = "internal"
	TypePush Type = "push"
	TypeSink Type = "sink"
	TypePull Type  = "pull"
	TypeSource Type = "source"
)

type Status struct {
	Type Type
	JobSpecific interface{}
}

func (s *Status) MarshalJSON() ([]byte, error) {
	typeJson, err := json.Marshal(s.Type)
	if err != nil {
		return nil, err
	}
	jobJSON, err := json.Marshal(s.JobSpecific)
	if err != nil {
		return nil, err
	}
	m := map[string]json.RawMessage {
		"type": typeJson,
		string(s.Type): jobJSON,
	}
	return json.Marshal(m)
}

func (s *Status) UnmarshalJSON(in []byte) (err error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(in, &m); err != nil {
		return err
	}
	tJSON, ok := m["type"]
	if !ok {
		return fmt.Errorf("field 'type' not found")
	}
	if err := json.Unmarshal(tJSON, &s.Type); err != nil {
		return err
	}
	key := string(s.Type)
	jobJSON, ok := m[key]
	if !ok {
		return fmt.Errorf("field '%s', not found", key)
	}
	switch s.Type {
	case TypePull: fallthrough
	case TypePush:
		var st ActiveSideStatus
		err = json.Unmarshal(jobJSON, &st)
		s.JobSpecific = &st
	case TypeSource: fallthrough
	case TypeSink:
		var st PassiveStatus
		err = json.Unmarshal(jobJSON, &st)
		s.JobSpecific = &st
	case TypeInternal:
		// internal jobs do not report specifics
	default:
		err = fmt.Errorf("unknown job type '%s'", key)
	}
	return err
}
