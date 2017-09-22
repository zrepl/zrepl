package cmd

import (
	"bytes"
	"fmt"
	"github.com/sirupsen/logrus"
	"os"
)

type CLIFormatter struct {
}

func (f CLIFormatter) Format(e *logrus.Entry) (out []byte, err error) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%s\n", e.Message)
	return buf.Bytes(), nil
}

var stdhookStderrLevels []logrus.Level = []logrus.Level{
	logrus.PanicLevel, logrus.FatalLevel, logrus.ErrorLevel, logrus.WarnLevel,
}

type Stdhook struct {
}

func NewStdHook() *Stdhook {
	return &Stdhook{}
}

func (h *Stdhook) Levels() []logrus.Level {
	// Accept all so we can filter the output later
	return []logrus.Level{
		logrus.PanicLevel, logrus.FatalLevel, logrus.ErrorLevel, logrus.WarnLevel,
		logrus.InfoLevel, logrus.DebugLevel,
	}
}

func (h *Stdhook) Fire(entry *logrus.Entry) error {
	s, err := entry.String()
	if err != nil {
		return err
	}
	for _, l := range stdhookStderrLevels {
		if l == entry.Level {
			fmt.Fprint(os.Stderr, s)
			return nil
		}
	}
	fmt.Fprint(os.Stdout, s)
	return nil
}

type nopWriter int

func (w nopWriter) Write(p []byte) (n int, err error) { return len(p), nil }
