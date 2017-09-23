package cmd

import (
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"os"
)

type LoggingConfig struct {
	Stdout struct {
		Level  logrus.Level
		Format LogFormat
	}
}

func parseLogging(i interface{}) (c *LoggingConfig, err error) {

	c = &LoggingConfig{}
	c.Stdout.Level = logrus.WarnLevel
	c.Stdout.Format = LogFormatHuman
	if i == nil {
		return c, nil
	}

	var asMap struct {
		Stdout struct {
			Level  string
			Format string
		}
	}
	if err = mapstructure.Decode(i, &asMap); err != nil {
		return nil, errors.Wrap(err, "mapstructure error")
	}

	if asMap.Stdout.Level != "" {
		lvl, err := logrus.ParseLevel(asMap.Stdout.Level)
		if err != nil {
			return nil, errors.Wrap(err, "cannot parse stdout log level")
		}
		c.Stdout.Level = lvl
	}
	if asMap.Stdout.Format != "" {
		format, err := parseLogFormat(asMap.Stdout.Format)
		if err != nil {
			return nil, errors.Wrap(err, "cannot parse log format")
		}
		c.Stdout.Format = format
	}

	return c, nil

}

type LogFormat string

const (
	LogFormatHuman  LogFormat = "human"
	LogFormatLogfmt LogFormat = "logfmt"
	LogFormatJSON   LogFormat = "json"
)

func (f LogFormat) Formatter() logrus.Formatter {
	switch f {
	case LogFormatHuman:
		return HumanFormatter{}
	case LogFormatLogfmt:
		return &logrus.TextFormatter{}
	case LogFormatJSON:
		return &logrus.JSONFormatter{}
	default:
		panic("incomplete implementation")
	}
}

var LogFormats []LogFormat = []LogFormat{LogFormatHuman, LogFormatLogfmt, LogFormatJSON}

func parseLogFormat(i interface{}) (f LogFormat, err error) {
	var is string
	switch j := i.(type) {
	case string:
		is = j
	default:
		return "", errors.Errorf("invalid log format: wrong type: %T", i)
	}

	for _, f := range LogFormats {
		if string(f) == is {
			return f, nil
		}
	}
	return "", errors.Errorf("invalid log format: '%s'", is)
}

func (c *LoggingConfig) MakeLogrus() (l logrus.FieldLogger) {

	log := logrus.New()
	log.Out = os.Stdout
	log.Level = c.Stdout.Level
	log.Formatter = c.Stdout.Format.Formatter()

	return log

}
