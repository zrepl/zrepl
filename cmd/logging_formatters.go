package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/logger"
	"strings"
	"time"
)

type EntryFormatter interface {
	Format(e *logger.Entry) ([]byte, error)
}

const (
	FieldLevel   = "level"
	FieldMessage = "msg"
	FieldTime    = "time"
)

const (
	logJobField     string = "job"
	logTaskField    string = "task"
	logFSField      string = "filesystem"
	logMapFromField string = "map_from"
	logMapToField   string = "map_to"
	logIncFromField string = "inc_from"
	logIncToField   string = "inc_to"
)

type NoFormatter struct{}

func (f NoFormatter) Format(e *logger.Entry) ([]byte, error) {
	return []byte(e.Message), nil
}

type HumanFormatter struct {
	NoMetadata bool
}

var _ SetNoMetadataFormatter = &HumanFormatter{}

func (f *HumanFormatter) SetNoMetadata(noMetadata bool) {
	f.NoMetadata = noMetadata
}

func (f *HumanFormatter) Format(e *logger.Entry) (out []byte, err error) {

	var line bytes.Buffer

	if !f.NoMetadata {
		fmt.Fprintf(&line, "[%s]", e.Level.Short())
	}

	prefixFields := []string{logJobField, logTaskField, logFSField}
	prefixed := make(map[string]bool, len(prefixFields)+2)
	for _, field := range prefixFields {
		val, ok := e.Fields[field].(string)
		if ok {
			fmt.Fprintf(&line, "[%s]", val)
			prefixed[field] = true
		} else {
			break
		}
	}
	// even more prefix fields
	mapFrom, mapFromOk := e.Fields[logMapFromField].(string)
	mapTo, mapToOk := e.Fields[logMapToField].(string)
	if mapFromOk && mapToOk {
		fmt.Fprintf(&line, "[%s => %s]", mapFrom, mapTo)
		prefixed[logMapFromField], prefixed[logMapToField] = true, true
	}
	incFrom, incFromOk := e.Fields[logIncFromField].(string)
	incTo, incToOk := e.Fields[logIncToField].(string)
	if incFromOk && incToOk {
		fmt.Fprintf(&line, "[%s => %s]", incFrom, incTo)
		prefixed[logIncFromField], prefixed[logIncToField] = true, true
	}

	if line.Len() > 0 {
		fmt.Fprint(&line, ": ")
	}
	fmt.Fprint(&line, e.Message)

	for field, value := range e.Fields {

		if prefixed[field] {
			continue
		}

		if strings.ContainsAny(field, " \t") {
			return nil, errors.Errorf("field must not contain whitespace: '%s'", field)
		}
		fmt.Fprintf(&line, " %s=\"%s\"", field, value)
	}

	return line.Bytes(), nil
}

type JSONFormatter struct{}

func (f *JSONFormatter) Format(e *logger.Entry) ([]byte, error) {
	data := make(logger.Fields, len(e.Fields)+3)
	for k, v := range e.Fields {
		switch v := v.(type) {
		case error:
			// Otherwise errors are ignored by `encoding/json`
			// https://github.com/sirupsen/logrus/issues/137
			data[k] = v.Error()
		default:
			_, err := json.Marshal(v)
			if err != nil {
				return nil, errors.Errorf("field is not JSON encodable: %s", k)
			}
			data[k] = v
		}
	}

	data[FieldMessage] = e.Message
	data[FieldTime] = e.Time.Format(time.RFC3339)
	data[FieldLevel] = e.Level

	return json.Marshal(data)

}
