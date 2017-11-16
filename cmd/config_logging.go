package cmd

import (
	"crypto/tls"
	"crypto/x509"
	"github.com/mattn/go-isatty"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/logger"
	"io/ioutil"
	"os"
	"time"
)

type LoggingConfig struct {
	Outlets *logger.Outlets
}

type MetadataFlags int64

const (
	MetadataTime MetadataFlags = 1 << iota
	MetadataLevel

	MetadataNone MetadataFlags = 0
	MetadataAll  MetadataFlags = ^0
)

func LoggerErrorOutlet() logger.Outlet {

	formatter := &HumanFormatter{}
	writer := os.Stderr
	formatter.SetMetadataFlags(MetadataAll)
	return WriterOutlet{
		formatter, writer,
	}
}

func parseLogging(i interface{}) (c *LoggingConfig, err error) {

	c = &LoggingConfig{}

	c.Outlets = logger.NewOutlets(LoggerErrorOutlet())

	var asList []interface{}
	if err = mapstructure.Decode(i, &asList); err != nil {
		return nil, errors.Wrap(err, "mapstructure error")
	}
	if len(asList) == 0 {
		// Default config
		out := WriterOutlet{&HumanFormatter{}, os.Stdout}
		c.Outlets.Add(out, logger.Warn)
		return
	}

	var syslogOutlets, stdoutOutlets int
	for lei, le := range asList {

		outlet, minLevel, err := parseOutlet(le)
		if err != nil {
			return nil, errors.Wrapf(err, "cannot parse outlet #%d", lei)
		}
		var _ logger.Outlet = WriterOutlet{}
		var _ logger.Outlet = &SyslogOutlet{}
		switch outlet.(type) {
		case *SyslogOutlet:
			syslogOutlets++
		case WriterOutlet:
			stdoutOutlets++
		}

		c.Outlets.Add(outlet, minLevel)

	}

	if syslogOutlets > 1 {
		return nil, errors.Errorf("can only define one 'syslog' outlet")
	}
	if stdoutOutlets > 1 {
		return nil, errors.Errorf("can only define one 'stdout' outlet")
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

func parseOutlet(i interface{}) (o logger.Outlet, level logger.Level, err error) {

	var in struct {
		Outlet string
		Level  string
		Format string
	}
	if err = mapstructure.Decode(i, &in); err != nil {
		err = errors.Wrap(err, "mapstructure error")
		return
	}
	if in.Outlet == "" || in.Level == "" || in.Format == "" {
		err = errors.Errorf("must specify 'outlet', 'level' and 'format' field")
		return
	}

	minLevel, err := logger.ParseLevel(in.Level)
	if err != nil {
		err = errors.Wrap(err, "cannot parse 'level' field")
		return
	}
	formatter, err := parseLogFormat(in.Format)
	if err != nil {
		err = errors.Wrap(err, "cannot parse")
		return
	}

	switch in.Outlet {
	case "stdout":
		o, err = parseStdoutOutlet(i, formatter)
	case "tcp":
		o, err = parseTCPOutlet(i, formatter)
	case "syslog":
		o, err = parseSyslogOutlet(i, formatter)
	default:
		err = errors.Errorf("unknown outlet type '%s'", in.Outlet)
	}
	return o, minLevel, err

}

func parseStdoutOutlet(i interface{}, formatter EntryFormatter) (WriterOutlet, error) {

	var in struct {
		Time bool
	}
	if err := mapstructure.Decode(i, &in); err != nil {
		return WriterOutlet{}, errors.Wrap(err, "invalid structure for stdout outlet")
	}

	flags := MetadataAll
	writer := os.Stdout
	if !isatty.IsTerminal(writer.Fd()) && !in.Time {
		flags &= ^MetadataTime
	}

	formatter.SetMetadataFlags(flags)
	return WriterOutlet{
		formatter,
		os.Stdout,
	}, nil
}

func parseTCPOutlet(i interface{}, formatter EntryFormatter) (out *TCPOutlet, err error) {

	out = &TCPOutlet{}
	out.Formatter = formatter
	out.Formatter.SetMetadataFlags(MetadataAll)

	var in struct {
		Net           string
		Address       string
		RetryInterval string `mapstructure:"retry_interval"`
		TLS           *struct {
			CA   string
			Cert string
			Key  string
		}
	}
	if err = mapstructure.Decode(i, &in); err != nil {
		return nil, errors.Wrap(err, "mapstructure error")
	}

	out.RetryInterval, err = time.ParseDuration(in.RetryInterval)
	if err != nil {
		return nil, errors.Wrap(err, "cannot parse 'retry_interval'")
	}

	out.Net, out.Address = in.Net, in.Address

	if in.TLS != nil {

		cert, err := tls.LoadX509KeyPair(in.TLS.Cert, in.TLS.Key)
		if err != nil {
			return nil, errors.Wrap(err, "cannot load client cert")
		}

		var rootCAs *x509.CertPool
		if in.TLS.CA == "" {
			if rootCAs, err = x509.SystemCertPool(); err != nil {
				return nil, errors.Wrap(err, "cannot open system cert pool")
			}
		} else {
			rootCAs = x509.NewCertPool()
			rootCAPEM, err := ioutil.ReadFile(in.TLS.CA)
			if err != nil {
				return nil, errors.Wrap(err, "cannot load CA cert")
			}
			if !rootCAs.AppendCertsFromPEM(rootCAPEM) {
				return nil, errors.New("cannot parse CA cert")
			}
		}
		if err != nil && in.TLS.CA == "" {
			return nil, errors.Wrap(err, "cannot load root ca pool")
		}

		out.TLS = &tls.Config{
			Certificates: []tls.Certificate{cert},
			RootCAs:      rootCAs,
		}

		out.TLS.BuildNameToCertificate()
	}

	return

}

func parseSyslogOutlet(i interface{}, formatter EntryFormatter) (out *SyslogOutlet, err error) {

	var in struct {
		RetryInterval string `mapstructure:"retry_interval"`
	}
	if err = mapstructure.Decode(i, &in); err != nil {
		return nil, errors.Wrap(err, "mapstructure error")
	}

	out = &SyslogOutlet{}
	out.Formatter = formatter
	out.Formatter.SetMetadataFlags(MetadataNone)

	out.RetryInterval = 0 // default to 0 as we assume local syslog will just work
	if in.RetryInterval != "" {
		out.RetryInterval, err = time.ParseDuration(in.RetryInterval)
		if err != nil {
			return nil, errors.Wrap(err, "cannot parse 'retry_interval'")
		}
	}

	return
}
