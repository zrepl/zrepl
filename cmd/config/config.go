package config

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/zrepl/yaml-config"
	"io/ioutil"
	"os"
	"regexp"
	"strconv"
	"time"
)

type Config struct {
	Jobs   []JobEnum `yaml:"jobs"`
	Global Global    `yaml:"global"`
}

type JobEnum struct {
	Ret interface{}
}

type PushJob struct {
	Type         string          `yaml:"type"`
	Replication  PushReplication `yaml:"replication"`
	Snapshotting Snapshotting    `yaml:"snapshotting"`
	Pruning      Pruning         `yaml:"pruning"`
}

type SinkJob struct {
	Type        string          `yaml:"type"`
	Replication SinkReplication `yaml:"replication"`
}

type PushReplication struct {
	Connect     ConnectEnum     `yaml:"connect"`
	Filesystems map[string]bool `yaml:"filesystems"`
}

type SinkReplication struct {
	RootDataset string    `yaml:"root_dataset"`
	Serve       ServeEnum `yaml:"serve"`
}

type Snapshotting struct {
	SnapshotPrefix string        `yaml:"snapshot_prefix"`
	Interval       time.Duration `yaml:"interval"`
}

type Pruning struct {
	KeepLocal  []PruningEnum `yaml:"keep_local"`
	KeepRemote []PruningEnum `yaml:"keep_remote"`
}

type Global struct {
	Logging []LoggingOutletEnum `yaml:"logging"`
}

type ConnectEnum struct {
	Ret interface{}
}

type TCPConnect struct {
	Type    string `yaml:"type"`
	Address string `yaml:"address"`
}

type TLSConnect struct {
	Type    string `yaml:"type"`
	Address string `yaml:"address"`
	Ca      string `yaml:"ca"`
	Cert    string `yaml:"cert"`
	Key     string `yaml:"key"`
}

type ServeEnum struct {
	Ret interface{}
}

type TCPServe struct {
	Type    string            `yaml:"type"`
	Listen  string            `yaml:"listen"`
	Clients map[string]string `yaml:"clients"`
}

type TLSServe struct {
	Type   string `yaml:"type"`
	Listen string `yaml:"listen"`
	Ca     string `yaml:"ca"`
	Cert   string `yaml:"cert"`
	Key    string `yaml:"key"`
}

type PruningEnum struct {
	Ret interface{}
}

type PruneKeepNotReplicated struct {
	Type string `yaml:"type"`
}

type PruneKeepLastN struct {
	Type  string `yaml:"type"`
	Count int    `yaml:"count"`
}

type LoggingOutletEnum struct {
	Ret interface{}
}

type LoggingOutletCommon struct {
	Type   string `yaml:"type"`
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type StdoutLoggingOutlet struct {
	LoggingOutletCommon `yaml:",inline"`
	Time                bool `yaml:"time"`
}

type SyslogLoggingOutlet struct {
	LoggingOutletCommon `yaml:",inline"`
	RetryInterval       time.Duration `yaml:"retry_interval"`
}

type TCPLoggingOutlet struct {
	LoggingOutletCommon `yaml:",inline"`
	Address             string        `yaml:"address"` //TODO required
	Net                 string        `yaml:"net"`     //TODO default tcp
	RetryInterval       time.Duration `yaml:"retry_interval"`
	TLS                 *TCPLoggingOutletTLS
}

type TCPLoggingOutletTLS struct {
	CA   string `yaml:"ca"`
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
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

func (t *JobEnum) UnmarshalYAML(u func(interface{}, bool) error) (err error) {
	t.Ret, err = enumUnmarshal(u, map[string]interface{}{
		"push": &PushJob{},
		"sink": &SinkJob{},
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
		"last_n":         &PruneKeepLastN{},
		"grid":           &PruneGrid{},
	})
	return
}

func (t *LoggingOutletEnum) UnmarshalYAML(u func(interface{}, bool) error) (err error) {
	t.Ret, err = enumUnmarshal(u, map[string]interface{}{
		"stdout": &StdoutLoggingOutlet{},
		"syslog": &SyslogLoggingOutlet{},
		"tcp":    &TCPLoggingOutlet{},
	})
	return
}

var ConfigFileDefaultLocations = []string{
	"/etc/zrepl/zrepl.yml",
	"/usr/local/etc/zrepl/zrepl.yml",
}

func ParseConfig(path string) (i Config, err error) {

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

var durationStringRegex *regexp.Regexp = regexp.MustCompile(`^\s*(\d+)\s*(s|m|h|d|w)\s*$`)

func parsePostitiveDuration(e string) (d time.Duration, err error) {
	comps := durationStringRegex.FindStringSubmatch(e)
	if len(comps) != 3 {
		err = fmt.Errorf("does not match regex: %s %#v", e, comps)
		return
	}

	durationFactor, err := strconv.ParseInt(comps[1], 10, 64)
	if err != nil {
		return 0, err
	}
	if durationFactor <= 0 {
		return 0, errors.New("duration must be positive integer")
	}

	var durationUnit time.Duration
	switch comps[2] {
	case "s":
		durationUnit = time.Second
	case "m":
		durationUnit = time.Minute
	case "h":
		durationUnit = time.Hour
	case "d":
		durationUnit = 24 * time.Hour
	case "w":
		durationUnit = 24 * 7 * time.Hour
	default:
		err = fmt.Errorf("contains unknown time unit '%s'", comps[2])
		return
	}

	d = time.Duration(durationFactor) * durationUnit
	return
}
