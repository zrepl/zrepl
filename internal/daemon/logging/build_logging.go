package logging

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"log/syslog"
	"os"

	"github.com/mattn/go-isatty"
	"github.com/pkg/errors"

	"github.com/LyingCak3/zrepl/internal/config"
	"github.com/LyingCak3/zrepl/internal/daemon/logging/trace"
	"github.com/LyingCak3/zrepl/internal/logger"
	"github.com/LyingCak3/zrepl/internal/tlsconf"
)

func OutletsFromConfig(in config.LoggingOutletEnumList) (*logger.Outlets, error) {

	outlets := logger.NewOutlets()

	if len(in) == 0 {
		// Default config
		out := WriterOutlet{&HumanFormatter{}, os.Stdout}
		outlets.Add(out, logger.Warn)
		return outlets, nil
	}

	var syslogOutlets, stdoutOutlets int
	for lei, le := range in {

		outlet, minLevel, err := ParseOutlet(le)
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

		outlets.Add(outlet, minLevel)

	}

	if syslogOutlets > 1 {
		return nil, errors.Errorf("can only define one 'syslog' outlet")
	}
	if stdoutOutlets > 1 {
		return nil, errors.Errorf("can only define one 'stdout' outlet")
	}

	return outlets, nil

}

type Subsystem string

const (
	SubsysMeta         Subsystem = "meta"
	SubsysJob          Subsystem = "job"
	SubsysReplication  Subsystem = "repl"
	SubsysEndpoint     Subsystem = "endpoint"
	SubsysPruning      Subsystem = "pruning"
	SubsysSnapshot     Subsystem = "snapshot"
	SubsysHooks        Subsystem = "hook"
	SubsysTransport    Subsystem = "transport"
	SubsysTransportMux Subsystem = "transportmux"
	SubsysRPC          Subsystem = "rpc"
	SubsysRPCControl   Subsystem = "rpc.ctrl"
	SubsysRPCData      Subsystem = "rpc.data"
	SubsysZFSCmd       Subsystem = "zfs.cmd"
	SubsysTraceData    Subsystem = "trace.data"
	SubsysPlatformtest Subsystem = "platformtest"
)

var AllSubsystems = []Subsystem{
	SubsysMeta,
	SubsysJob,
	SubsysReplication,
	SubsysEndpoint,
	SubsysPruning,
	SubsysSnapshot,
	SubsysHooks,
	SubsysTransport,
	SubsysTransportMux,
	SubsysRPC,
	SubsysRPCControl,
	SubsysRPCData,
	SubsysZFSCmd,
	SubsysTraceData,
	SubsysPlatformtest,
}

type injectedField struct {
	field  string
	value  interface{}
	parent *injectedField
}

func WithInjectedField(ctx context.Context, field string, value interface{}) context.Context {
	var parent *injectedField
	parentI := ctx.Value(contextKeyInjectedField)
	if parentI != nil {
		parent = parentI.(*injectedField)
	}
	// TODO sanity-check `field` now
	this := &injectedField{field, value, parent}
	return context.WithValue(ctx, contextKeyInjectedField, this)
}

func iterInjectedFields(ctx context.Context, cb func(field string, value interface{})) {
	injI := ctx.Value(contextKeyInjectedField)
	if injI == nil {
		return
	}
	inj := injI.(*injectedField)
	for ; inj != nil; inj = inj.parent {
		cb(inj.field, inj.value)
	}
}

type SubsystemLoggers map[Subsystem]logger.Logger

func SubsystemLoggersWithUniversalLogger(l logger.Logger) SubsystemLoggers {
	loggers := make(SubsystemLoggers)
	for _, s := range AllSubsystems {
		loggers[s] = l
	}
	return loggers
}

func WithLoggers(ctx context.Context, loggers SubsystemLoggers) context.Context {
	return context.WithValue(ctx, contextKeyLoggers, loggers)
}

func GetLoggers(ctx context.Context) SubsystemLoggers {
	loggers, ok := ctx.Value(contextKeyLoggers).(SubsystemLoggers)
	if !ok {
		return nil
	}
	return loggers
}

func GetLogger(ctx context.Context, subsys Subsystem) logger.Logger {
	return getLoggerImpl(ctx, subsys, true)
}

func getLoggerImpl(ctx context.Context, subsys Subsystem, panicIfEnded bool) logger.Logger {
	loggers, ok := ctx.Value(contextKeyLoggers).(SubsystemLoggers)
	if !ok || loggers == nil {
		return logger.NewNullLogger()
	}
	l, ok := loggers[subsys]
	if !ok {
		return logger.NewNullLogger()
	}

	l = l.WithField(SubsysField, subsys)

	l = l.WithField(SpanField, trace.GetSpanStackOrDefault(ctx, *trace.StackKindId, "NOSPAN"))

	fields := make(logger.Fields)
	iterInjectedFields(ctx, func(field string, value interface{}) {
		fields[field] = value
	})
	l = l.WithFields(fields)

	return l
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

func ParseOutlet(in config.LoggingOutletEnum) (o logger.Outlet, level logger.Level, err error) {

	parseCommon := func(common config.LoggingOutletCommon) (logger.Level, EntryFormatter, error) {
		if common.Level == "" || common.Format == "" {
			return 0, nil, errors.Errorf("must specify 'level' and 'format' field")
		}

		minLevel, err := logger.ParseLevel(common.Level)
		if err != nil {
			return 0, nil, errors.Wrap(err, "cannot parse 'level' field")
		}
		formatter, err := parseLogFormat(common.Format)
		if err != nil {
			return 0, nil, errors.Wrap(err, "cannot parse 'formatter' field")
		}
		return minLevel, formatter, nil
	}

	var f EntryFormatter

	switch v := in.Ret.(type) {
	case *config.StdoutLoggingOutlet:
		level, f, err = parseCommon(v.LoggingOutletCommon)
		if err != nil {
			break
		}
		o, err = parseStdoutOutlet(v, f)
	case *config.TCPLoggingOutlet:
		level, f, err = parseCommon(v.LoggingOutletCommon)
		if err != nil {
			break
		}
		o, err = parseTCPOutlet(v, f)
	case *config.SyslogLoggingOutlet:
		level, f, err = parseCommon(v.LoggingOutletCommon)
		if err != nil {
			break
		}
		o, err = parseSyslogOutlet(v, f)
	default:
		panic(v)
	}
	return o, level, err
}

func parseStdoutOutlet(in *config.StdoutLoggingOutlet, formatter EntryFormatter) (WriterOutlet, error) {
	flags := MetadataAll
	writer := os.Stdout
	if !isatty.IsTerminal(writer.Fd()) && !in.Time {
		flags &= ^MetadataTime
	}
	if isatty.IsTerminal(writer.Fd()) && !in.Color {
		flags &= ^MetadataColor
	}

	formatter.SetMetadataFlags(flags)
	return WriterOutlet{
		formatter,
		os.Stdout,
	}, nil
}

func parseTCPOutlet(in *config.TCPLoggingOutlet, formatter EntryFormatter) (out *TCPOutlet, err error) {
	var tlsConfig *tls.Config
	if in.TLS != nil {
		tlsConfig, err = func(m *config.TCPLoggingOutletTLS, host string) (*tls.Config, error) {
			clientCert, err := tls.LoadX509KeyPair(m.Cert, m.Key)
			if err != nil {
				return nil, errors.Wrap(err, "cannot load client cert")
			}

			var rootCAs *x509.CertPool
			if m.CA == "" {
				if rootCAs, err = x509.SystemCertPool(); err != nil {
					return nil, errors.Wrap(err, "cannot open system cert pool")
				}
			} else {
				rootCAs, err = tlsconf.ParseCAFile(m.CA)
				if err != nil {
					return nil, errors.Wrap(err, "cannot parse CA cert")
				}
			}
			if rootCAs == nil {
				panic("invariant violated")
			}

			return tlsconf.ClientAuthClient(host, rootCAs, clientCert)
		}(in.TLS, in.Address)
		if err != nil {
			return nil, errors.New("cannot not parse TLS config in field 'tls'")
		}
	}

	formatter.SetMetadataFlags(MetadataAll)
	return NewTCPOutlet(formatter, in.Net, in.Address, tlsConfig, in.RetryInterval), nil

}

func parseSyslogOutlet(in *config.SyslogLoggingOutlet, formatter EntryFormatter) (out *SyslogOutlet, err error) {
	out = &SyslogOutlet{}
	out.Formatter = formatter
	out.Formatter.SetMetadataFlags(MetadataNone)
	out.Facility = syslog.Priority(*in.Facility)
	out.RetryInterval = in.RetryInterval
	return out, nil
}
