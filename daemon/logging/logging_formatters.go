package logging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/fatih/color"
	"github.com/go-logfmt/logfmt"
	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/logger"
)

const (
	FieldLevel   = "level"
	FieldMessage = "msg"
	FieldTime    = "time"
)

const (
	JobField    string = "job"
	SubsysField string = "subsystem"
)

type MetadataFlags int64

const (
	MetadataTime MetadataFlags = 1 << iota
	MetadataLevel
	MetadataColor

	MetadataNone MetadataFlags = 0
	MetadataAll  MetadataFlags = ^0
)

type NoFormatter struct{}

func (f NoFormatter) SetMetadataFlags(flags MetadataFlags) {}

func (f NoFormatter) Format(e *logger.Entry) ([]byte, error) {
	return []byte(e.Message), nil
}

type HumanFormatter struct {
	metadataFlags MetadataFlags
	ignoreFields  map[string]bool
}

const HumanFormatterDateFormat = time.RFC3339

func (f *HumanFormatter) SetMetadataFlags(flags MetadataFlags) {
	f.metadataFlags = flags
}

func (f *HumanFormatter) SetIgnoreFields(ignore []string) {
	if ignore == nil {
		f.ignoreFields = nil
		return
	}
	f.ignoreFields = make(map[string]bool, len(ignore))

	for _, field := range ignore {
		f.ignoreFields[field] = true
	}
}

func (f *HumanFormatter) ignored(field string) bool {
	return f.ignoreFields != nil && f.ignoreFields[field]
}

func (f *HumanFormatter) Format(e *logger.Entry) (out []byte, err error) {

	var line bytes.Buffer
	col := color.New()
	if f.metadataFlags&MetadataColor != 0 {
		col = e.Color()
	}

	if f.metadataFlags&MetadataTime != 0 {
		fmt.Fprintf(&line, "%s ", e.Time.Format(HumanFormatterDateFormat))
	}
	if f.metadataFlags&MetadataLevel != 0 {
		fmt.Fprintf(&line, "[%s]", col.Sprint(e.Level.Short()))
	}

	prefixFields := []string{JobField, SubsysField}
	prefixed := make(map[string]bool, len(prefixFields)+2)
	for _, field := range prefixFields {
		val, ok := e.Fields[field]
		if !ok {
			continue
		}
		if !f.ignored(field) {
			fmt.Fprintf(&line, "[%s]", col.Sprint(val))
			prefixed[field] = true
		}
	}

	if line.Len() > 0 {
		fmt.Fprint(&line, ": ")
	}
	fmt.Fprint(&line, e.Message)

	if len(e.Fields)-len(prefixed) > 0 {
		for field, value := range e.Fields {
			if prefixed[field] || f.ignored(field) {
				continue
			}
			fmt.Fprintf(&line, " %s=%q", col.Sprint(field), fmt.Sprint(value))
		}
	}

	return line.Bytes(), nil
}

type JSONFormatter struct {
	metadataFlags MetadataFlags
}

func (f *JSONFormatter) SetMetadataFlags(flags MetadataFlags) {
	f.metadataFlags = flags
}

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

type LogfmtFormatter struct {
	metadataFlags MetadataFlags
}

func (f *LogfmtFormatter) SetMetadataFlags(flags MetadataFlags) {
	f.metadataFlags = flags
}

func (f *LogfmtFormatter) Format(e *logger.Entry) ([]byte, error) {
	var buf bytes.Buffer
	enc := logfmt.NewEncoder(&buf)

	if f.metadataFlags&MetadataTime != 0 {
		err := enc.EncodeKeyval(FieldTime, e.Time)
		if err != nil {
			return nil, errors.Wrap(err, "logfmt: encode time")
		}
	}
	if f.metadataFlags&MetadataLevel != 0 {
		err := enc.EncodeKeyval(FieldLevel, e.Level)
		if err != nil {
			return nil, errors.Wrap(err, "logfmt: encode level")
		}
	}

	// at least try and put job and task in front
	prefixed := make(map[string]bool, 2)
	prefix := []string{JobField, SubsysField}
	for _, pf := range prefix {
		v, ok := e.Fields[pf]
		if !ok {
			break
		}
		if err := logfmtTryEncodeKeyval(enc, pf, v); err != nil {
			return nil, err // unlikely
		}
		prefixed[pf] = true
	}

	err := enc.EncodeKeyval(FieldMessage, e.Message)
	if err != nil {
		return nil, errors.Wrap(err, "logfmt: encode message")
	}
	for k, v := range e.Fields {
		if !prefixed[k] {
			if err := logfmtTryEncodeKeyval(enc, k, v); err != nil {
				return nil, err
			}
		}
	}

	return buf.Bytes(), nil
}

func logfmtTryEncodeKeyval(enc *logfmt.Encoder, field, value interface{}) error {

	err := enc.EncodeKeyval(field, value)
	switch err {
	case nil: // ok
		return nil
	case logfmt.ErrUnsupportedValueType:
		err := enc.EncodeKeyval(field, fmt.Sprintf("<%T>", value))
		if err != nil {
			return errors.Wrap(err, "cannot encode unsupported value type Go type")
		}
		return nil
	}
	return errors.Wrapf(err, "cannot encode field '%s'", field)

}
