package cmd

import (
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/logger"
	"os"
	"time"
)

type LoggingConfig struct {
	Outlets logger.Outlets
}

type SetNoMetadataFormatter interface {
	SetNoMetadata(noMetadata bool)
}

func parseLogging(i interface{}) (c *LoggingConfig, err error) {

	c = &LoggingConfig{}
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
		Syslog struct {
			Enable        bool
			Format        string
			RetryInterval string `mapstructure:"retry_interval"`
		}
	}
	if err = mapstructure.Decode(i, &asMap); err != nil {
		return nil, errors.Wrap(err, "mapstructure error")
	}

	c.Outlets = logger.NewOutlets()

	if asMap.Stdout.Level != "" {

		out := WriterOutlet{
			&HumanFormatter{},
			os.Stdout,
		}

		level, err := logger.ParseLevel(asMap.Stdout.Level)
		if err != nil {
			return nil, errors.Wrap(err, "cannot parse 'level'")
		}

		if asMap.Stdout.Format != "" {
			out.Formatter, err = parseLogFormat(asMap.Stdout.Format)
			if err != nil {
				return nil, errors.Wrap(err, "cannot parse 'format'")
			}
		}

		c.Outlets.Add(out, level)

	}

	if asMap.TCP.Address != "" {

		out := &TCPOutlet{}

		out.Formatter, err = parseLogFormat(asMap.TCP.Format)
		if err != nil {
			return nil, errors.Wrap(err, "cannot parse 'format'")
		}

		lvl, err := logger.ParseLevel(asMap.TCP.Level)
		if err != nil {
			return nil, errors.Wrap(err, "cannot parse 'level'")
		}

		out.RetryInterval, err = time.ParseDuration(asMap.TCP.RetryInterval)
		if err != nil {
			return nil, errors.Wrap(err, "cannot parse 'retry_interval'")
		}

		out.Net, out.Address = asMap.TCP.Net, asMap.TCP.Address

		c.Outlets.Add(out, lvl)
	}

	if asMap.Syslog.Enable {

		out := &SyslogOutlet{}

		out.Formatter = &HumanFormatter{}
		if asMap.Syslog.Format != "" {
			out.Formatter, err = parseLogFormat(asMap.Syslog.Format)
			if err != nil {
				return nil, errors.Wrap(err, "cannot parse 'format'")
			}
		}

		if f, ok := out.Formatter.(SetNoMetadataFormatter); ok {
			f.SetNoMetadata(true)
		}

		out.RetryInterval = 0 // default to 0 as we assume local syslog will just work
		if asMap.Syslog.RetryInterval != "" {
			out.RetryInterval, err = time.ParseDuration(asMap.Syslog.RetryInterval)
			if err != nil {
				return nil, errors.Wrap(err, "cannot parse 'retry_interval'")
			}
		}

		c.Outlets.Add(out, logger.Debug)

	}

	return c, nil

}

func parseLogFormat(i interface{}) (f EntryFormatter, err error) {
	var is string
	switch j := i.(type) {
	case string:
		is = j
	default:
		return nil, errors.Errorf("invalid log format: wrong type: %T", i)
	}

	switch is {
	case "human":
		return &HumanFormatter{}, nil
	case "logfmt":
		return &LogfmtFormatter{}, nil
	case "json":
		return &JSONFormatter{}, nil
	default:
		return nil, errors.Errorf("invalid log format: '%s'", is)
	}

}
