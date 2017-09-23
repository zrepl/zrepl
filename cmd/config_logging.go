package cmd

import (
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"os"
	"time"
)

type LoggingConfig struct {
	Stdout struct {
		Level  logrus.Level
		Format LogFormat
	}
	TCP *TCPLoggingConfig
}

type TCPLoggingConfig struct {
	Level         logrus.Level
	Format        LogFormat
	Net           string
	Address       string
	RetryInterval time.Duration
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
		TCP struct {
			Level         string
			Format        string
			Net           string
			Address       string
			RetryInterval string `mapstructure:"retry_interval"`
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

	if asMap.TCP.Address != "" {
		c.TCP = &TCPLoggingConfig{}
		c.TCP.Format, err = parseLogFormat(asMap.TCP.Format)
		if err != nil {
			return nil, errors.Wrap(err, "cannot parse log format")
		}
		c.TCP.Level, err = logrus.ParseLevel(asMap.TCP.Level)
		if err != nil {
			return nil, errors.Wrap(err, "cannot parse level")
		}
		c.TCP.RetryInterval, err = time.ParseDuration(asMap.TCP.RetryInterval)
		c.TCP.Net, c.TCP.Address = asMap.TCP.Net, asMap.TCP.Address
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
	log.Level = logrus.DebugLevel // FIXTHIS IN LOGRUS
	log.Formatter = c.Stdout.Format.Formatter()

	th := &TCPHook{Formatter: JSONFormatter{}, MinLevel: c.TCP.Level, Net: c.TCP.Net, Address: c.TCP.Address, RetryInterval: c.TCP.RetryInterval}
	log.Hooks.Add(th)

	return log

}
