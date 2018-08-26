package cmd

import (
	"net"

	"fmt"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/zfs"
)

type Config struct {
	Global Global
	Jobs   map[string]Job
}

func (c *Config) LookupJob(name string) (j Job, err error) {
	j, ok := c.Jobs[name]
	if !ok {
		return nil, errors.Errorf("job '%s' is not defined", name)
	}
	return j, nil
}

type Global struct {
	Serve struct {
		Stdinserver struct {
			SockDir string
		}
	}
	Control struct {
		Sockpath string
	}
	logging *LoggingConfig
}

type JobDebugSettings struct {
	Conn struct {
		ReadDump  string `mapstructure:"read_dump"`
		WriteDump string `mapstructure:"write_dump"`
	}
	RPC struct {
		Log bool
	}
}

type ListenerFactory interface {
	Listen() (net.Listener, error)
}

type SSHStdinServerConnectDescr struct {
}

type PrunePolicy interface {
	// Prune filters versions and decide which to keep and which to remove.
	// Prune **does not** implement the actual removal of the versions.
	Prune(fs *zfs.DatasetPath, versions []zfs.FilesystemVersion) (keep, remove []zfs.FilesystemVersion, err error)
}

type PruningJob interface {
	Pruner(side PrunePolicySide, dryRun bool) (Pruner, error)
}

// A type for constants describing different prune policies of a PruningJob
// This is mostly a special-case for LocalJob, which is the only job that has two prune policies
// instead of one.
// It implements github.com/spf13/pflag.Value to be used as CLI flag for the test subcommand
type PrunePolicySide string

const (
	PrunePolicySideDefault PrunePolicySide = ""
	PrunePolicySideLeft    PrunePolicySide = "left"
	PrunePolicySideRight   PrunePolicySide = "right"
)

func (s *PrunePolicySide) String() string {
	return string(*s)
}

func (s *PrunePolicySide) Set(news string) error {
	p := PrunePolicySide(news)
	switch p {
	case PrunePolicySideRight:
		fallthrough
	case PrunePolicySideLeft:
		*s = p
	default:
		return errors.Errorf("must be either %s or %s", PrunePolicySideLeft, PrunePolicySideRight)
	}
	return nil
}

func (s *PrunePolicySide) Type() string {
	return fmt.Sprintf("%s | %s", PrunePolicySideLeft, PrunePolicySideRight)
}
