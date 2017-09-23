package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"net"
	"strings"
	"time"
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

func (f NoFormatter) Format(e *logrus.Entry) ([]byte, error) {
	return []byte(e.Message), nil
}

type HumanFormatter struct{}

func (f HumanFormatter) shortLevel(l logrus.Level) string {
	switch l {
	case logrus.DebugLevel:
		return "DBG"
	case logrus.InfoLevel:
		return "INF"
	case logrus.WarnLevel:
		return "WRN"
	case logrus.ErrorLevel:
		return "ERR"
	case logrus.PanicLevel:
		return "PNC"
	}
	panic("incomplete implementation")
}

func (f HumanFormatter) Format(e *logrus.Entry) (out []byte, err error) {

	var line bytes.Buffer

	fmt.Fprintf(&line, "[%s]", f.shortLevel(e.Level))

	prefixFields := []string{logJobField, logTaskField, logFSField}
	prefixed := make(map[string]bool, len(prefixFields)+2)
	for _, field := range prefixFields {
		val, ok := e.Data[field].(string)
		if ok {
			fmt.Fprintf(&line, "[%s]", val)
			prefixed[field] = true
		} else {
			break
		}
	}
	// even more prefix fields
	mapFrom, mapFromOk := e.Data[logMapFromField].(string)
	mapTo, mapToOk := e.Data[logMapToField].(string)
	if mapFromOk && mapToOk {
		fmt.Fprintf(&line, "[%s => %s]", mapFrom, mapTo)
		prefixed[logMapFromField], prefixed[logMapToField] = true, true
	}
	incFrom, incFromOk := e.Data[logIncFromField].(string)
	incTo, incToOk := e.Data[logIncToField].(string)
	if incFromOk && incToOk {
		fmt.Fprintf(&line, "[%s => %s]", incFrom, incTo)
		prefixed[logIncFromField], prefixed[logIncToField] = true, true
	}

	fmt.Fprintf(&line, ": %s", e.Message)

	for field, value := range e.Data {

		if prefixed[field] {
			continue
		}

		if strings.ContainsAny(field, " \t") {
			return nil, errors.Errorf("field must not contain whitespace: '%s'", field)
		}
		fmt.Fprintf(&line, " %s=\"%s\"", field, value)
	}

	fmt.Fprintf(&line, "\n")

	return line.Bytes(), nil
}

type JSONFormatter struct{}

func (f JSONFormatter) Format(e *logrus.Entry) ([]byte, error) {
	data := make(logrus.Fields, len(e.Data)+3)
	for k, v := range e.Data {
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

	data["msg"] = e.Message
	data["time"] = e.Time.Format(time.RFC3339)
	data["level"] = e.Level

	return json.Marshal(data)

}

type TCPHook struct {
	Formatter     logrus.Formatter
	MinLevel      logrus.Level
	Net, Address  string
	RetryInterval time.Duration
	conn          net.Conn
	retry         time.Time
}

func (h *TCPHook) Levels() []logrus.Level {
	for i := range logrus.AllLevels { // assume it's ordered
		if logrus.AllLevels[i] == h.MinLevel {
			return logrus.AllLevels[:i]
		}
	}
	return logrus.AllLevels
}

func (h *TCPHook) Fire(e *logrus.Entry) error {
	b, err := h.Formatter.Format(e)
	if err != nil {
		return err
	}

	if h.conn == nil {
		if time.Now().Sub(h.retry) < h.RetryInterval {
			return errors.New("TCP hook reconnect prohibited by retry interval")
		}
		h.conn, err = net.Dial(h.Net, h.Address)
		if err != nil {
			h.retry = time.Now()
			return errors.Wrap(err, "cannot dial")
		}
	}

	_, err = h.conn.Write(b)
	if err != nil {
		h.conn.Close()
		h.conn = nil
	}

	return nil
}

type nopWriter int

func (w nopWriter) Write(p []byte) (n int, err error) { return len(p), nil }
