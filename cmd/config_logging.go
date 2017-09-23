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
	}
	if err = mapstructure.Decode(i, &asMap); err != nil {
		return nil, errors.Wrap(err, "mapstructure error")
	}

	c.Outlets = logger.NewOutlets()

	if asMap.Stdout.Level != "" {

		out := WriterOutlet{
			HumanFormatter{},
			os.Stdout,
		}

		level, err := logger.ParseLevel(asMap.Stdout.Level)
		if err != nil {
			return nil, errors.Wrap(err, "cannot parse stdout log level")
		}

		if asMap.Stdout.Format != "" {
			out.Formatter, err = parseLogFormat(asMap.Stdout.Format)
			if err != nil {
				return nil, errors.Wrap(err, "cannot parse log format")
			}
		}

		c.Outlets.Add(out, level)

	}

	if asMap.TCP.Address != "" {

		out := &TCPOutlet{}

		out.Formatter, err = parseLogFormat(asMap.TCP.Format)
		if err != nil {
			return nil, errors.Wrap(err, "cannot parse log 'format'")
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
		return HumanFormatter{}, nil
	case "json":
		return &JSONFormatter{}, nil
	default:
		return nil, errors.Errorf("invalid log format: '%s'", is)
	}

}
