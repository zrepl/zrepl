package config

import (
	"github.com/zrepl/yaml-config"
	"fmt"
	"time"
	"os"
	"github.com/pkg/errors"
	"io/ioutil"
)

type NodeEnum struct {
	Ret interface{}
}

type PushNode struct {
	Type string `yaml:"type"`
	Replication PushReplication `yaml:"replication"`
	Snapshotting Snapshotting `yaml:"snapshotting"`
	Pruning Pruning `yaml:"pruning"`
	Global Global `yaml:"global"`
}

type SinkNode struct {
	Type string `yaml:"type"`
	Replication SinkReplication `yaml:"replication"`
	Global Global `yaml:"global"`
}

type PushReplication struct {
	Connect ConnectEnum `yaml:"connect"`
	Filesystems map[string]bool `yaml:"filesystems"`
}

type SinkReplication struct {
	RootDataset string `yaml:"root_dataset"`
	Serve ServeEnum `yaml:"serve"`
}

type Snapshotting struct {
	SnapshotPrefix string `yaml:"snapshot_prefix"`
	Interval time.Duration `yaml:"interval"`
}

type Pruning struct {
	KeepLocal []PruningEnum `yaml:"keep_local"`
	KeepRemote []PruningEnum `yaml:"keep_remote"`
}

type Global struct {
	Logging []LoggingOutlet `yaml:"logging"`
}

type ConnectEnum struct {
	Ret interface{}
}

type TCPConnect struct {
	Type string `yaml:"type"`
	Address string `yaml:"address"`
}

type TLSConnect struct {
	Type string `yaml:"type"`
	Address string `yaml:"address"`
	Ca string `yaml:"ca"`
	Cert string `yaml:"cert"`
	Key string `yaml:"key"`
}

type ServeEnum struct {
	Ret interface{}
}

type TCPServe struct {
	Type string `yaml:"type"`
	Listen string `yaml:"listen"`
	Clients map[string]string `yaml:"clients"`
}

type TLSServe struct {
	Type string `yaml:"type"`
	Listen string `yaml:"listen"`
	Ca string `yaml:"ca"`
	Cert string `yaml:"cert"`
	Key string `yaml:"key"`
}

type PruningEnum struct {
	Ret interface{}
}

type PruneKeepNotReplicated struct {
	Type string `yaml:"type"`
}

type PruneKeepLastN struct {
	Type string `yaml:"type"`
	Count int `yaml:"count"`
}

type PruneGrid struct {
	Type string `yaml:"type"`
	Grid string `yaml:"grid"`
}

type LoggingOutlet struct {
	Outlet LoggingOutletEnum `yaml:"outlet"`
	Level string `yaml:"level"`
	Format string `yaml:"format"`
}

type LoggingOutletEnum struct {
	Ret interface{}
}

type StdoutLoggingOutlet struct {
	Type string `yaml:"type"`
	Time bool `yaml:"time"`
}

type SyslogLoggingOutlet struct {
	Type string `yaml:"type"`
	RetryInterval time.Duration `yaml:"retry_interval"`
}

func enumUnmarshal(u func(interface{}, bool) error, types map[string]interface{}) (interface{}, error) {
	var in struct {
		Type string
	}
	if err := u(&in, true); err != nil {
		return nil, err
	}
	if in.Type == "" {
		return nil, &yaml.TypeError{[]string{"must specify type"}}
	}

	v, ok := types[in.Type]
	if !ok {
		return nil, &yaml.TypeError{[]string{fmt.Sprintf("invalid type name %q", in.Type)}}
	}
	if err := u(v, false); err != nil {
		return nil, err
	}
	return v, nil
}

func (t *NodeEnum) UnmarshalYAML(u func(interface{}, bool) error) (err error) {
	t.Ret, err = enumUnmarshal(u, map[string]interface{}{
		"push": &PushNode{},
		"sink": &SinkNode{},
	})
	return
}

func (t *ConnectEnum) UnmarshalYAML(u func(interface{}, bool) error) (err error) {
	t.Ret, err = enumUnmarshal(u, map[string]interface{}{
		"tcp": &TCPConnect{},
		"tls": &TLSConnect{},
	})
	return
}

func (t *ServeEnum) UnmarshalYAML(u func(interface{}, bool) error) (err error) {
	t.Ret, err = enumUnmarshal(u, map[string]interface{}{
		"tcp": &TCPServe{},
		"tls": &TLSServe{},
	})
	return
}

func (t *PruningEnum) UnmarshalYAML(u func(interface{}, bool) error) (err error) {
	t.Ret, err = enumUnmarshal(u, map[string]interface{}{
		"not_replicated": &PruneKeepNotReplicated{},
		"last_n": &PruneKeepLastN{},
		"grid": &PruneGrid{},
	})
	return
}

func (t *LoggingOutletEnum) UnmarshalYAML(u func(interface{}, bool) error) (err error) {
	t.Ret, err = enumUnmarshal(u, map[string]interface{}{
		"stdout": &StdoutLoggingOutlet{},
		"syslog": &SyslogLoggingOutlet{},
	})
	return
}

var ConfigFileDefaultLocations = []string{
	"/etc/zrepl/zrepl.yml",
	"/usr/local/etc/zrepl/zrepl.yml",
}

func ParseConfig(path string) (i NodeEnum, err error) {

	if path == "" {
		// Try default locations
		for _, l := range ConfigFileDefaultLocations {
			stat, statErr := os.Stat(l)
			if statErr != nil {
				continue
			}
			if !stat.Mode().IsRegular() {
				err = errors.Errorf("file at default location is not a regular file: %s", l)
				return
			}
			path = l
			break
		}
	}

	var bytes []byte

	if bytes, err = ioutil.ReadFile(path); err != nil {
		return
	}

	if err = yaml.UnmarshalStrict(bytes, &i); err != nil {
		return
	}

	return
}
