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
	Type         string                `yaml:"type"`
	Name         string                `yaml:"name"`
	Replication  PushReplication       `yaml:"replication"`
	Snapshotting Snapshotting          `yaml:"snapshotting"`
	Pruning      PruningSenderReceiver `yaml:"pruning"`
}

type SinkJob struct {
	Type        string          `yaml:"type"`
	Name        string          `yaml:"name"`
	Replication SinkReplication `yaml:"replication"`
}

type PullJob struct {
	Type        string                `yaml:"type"`
	Name        string                `yaml:"name"`
	Replication PullReplication       `yaml:"replication"`
	Pruning     PruningSenderReceiver `yaml:"pruning"`
}

type PullReplication struct {
	Connect     ConnectEnum `yaml:"connect"`
	RootDataset string      `yaml:"root_dataset"`
}

type SourceJob struct {
	Type         string            `yaml:"type"`
	Name         string            `yaml:"name"`
	Replication  SourceReplication `yaml:"replication"`
	Snapshotting Snapshotting      `yaml:"snapshotting"`
	Pruning      PruningLocal      `yaml:"pruning"`
}

type LocalJob struct {
	Type        string                `yaml:"type"`
	Name        string                `yaml:"name"`
	Replication LocalReplication       `yaml:"replication"`
	Snapshotting Snapshotting          `yaml:"snapshotting"`
	Pruning     PruningSenderReceiver `yaml:"pruning"`
}

type PushReplication struct {
	Connect     ConnectEnum     `yaml:"connect"`
	Filesystems map[string]bool `yaml:"filesystems"`
}

type SinkReplication struct {
	RootDataset string    `yaml:"root_dataset"`
	Serve       ServeEnum `yaml:"serve"`
}

type SourceReplication struct {
	Serve       ServeEnum       `yaml:"serve"`
	Filesystems map[string]bool `yaml:"filesystems"`
}

type LocalReplication struct {
	Filesystems map[string]bool `yaml:"filesystems"`
	RootDataset string    `yaml:"root_dataset"`
}

type Snapshotting struct {
	SnapshotPrefix string        `yaml:"snapshot_prefix"`
	Interval       time.Duration `yaml:"interval"`
}

type PruningSenderReceiver struct {
	KeepSender   []PruningEnum `yaml:"keep_sender"`
	KeepReceiver []PruningEnum `yaml:"keep_receiver"`
}

type PruningLocal struct {
	Keep []PruningEnum `yaml:"keep"`
}

type Global struct {
	Logging    []LoggingOutletEnum `yaml:"logging"`
	Monitoring []MonitoringEnum    `yaml:"monitoring,optional"`
	Control    GlobalControl       `yaml:"control"`
	Serve      GlobalServe         `yaml:"serve"`
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

type SSHStdinserverConnect struct {
	Type string `yaml:"type"`
	Host string `yaml:"host"`
	User string `yaml:"user"`
	Port uint16 `yaml:"port"`
	IdentityFile string `yaml:"identity_file"`
	Options []string `yaml:"options"`
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

type StdinserverServer struct {
	Type string `yaml:"type"`
	ClientIdentity string `yaml:"client_identity"`
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
	Address             string               `yaml:"address"`
	Net                 string               `yaml:"net,default=tcp"`
	RetryInterval       time.Duration        `yaml:"retry_interval"`
	TLS                 *TCPLoggingOutletTLS `yaml:"tls,optional"`
}

type TCPLoggingOutletTLS struct {
	CA   string `yaml:"ca"`
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

type MonitoringEnum struct {
	Ret interface{}
}

type PrometheusMonitoring struct {
	Type   string `yaml:"type"`
	Listen string `yaml:"listen"`
}

type GlobalControl struct {
	SockPath string `yaml:"sockpath,default=/var/run/zrepl/control"`
}

type GlobalServe struct {
	StdinServer GlobalStdinServer `yaml:"stdinserver"`
}

type GlobalStdinServer struct {
	SockDir string `yaml:"sockdir,default=/var/run/zrepl/stdinserver"`
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
		"push":   &PushJob{},
		"sink":   &SinkJob{},
		"pull":   &PullJob{},
		"source": &SourceJob{},
		"local": &LocalJob{},
	})
	return
}

func (t *ConnectEnum) UnmarshalYAML(u func(interface{}, bool) error) (err error) {
	t.Ret, err = enumUnmarshal(u, map[string]interface{}{
		"tcp": &TCPConnect{},
		"tls": &TLSConnect{},
		"ssh+stdinserver": &SSHStdinserverConnect{},
	})
	return
}

func (t *ServeEnum) UnmarshalYAML(u func(interface{}, bool) error) (err error) {
	t.Ret, err = enumUnmarshal(u, map[string]interface{}{
		"tcp": &TCPServe{},
		"tls": &TLSServe{},
		"stdinserver": &StdinserverServer{},
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

func (t *MonitoringEnum) UnmarshalYAML(u func(interface{}, bool) error) (err error) {
	t.Ret, err = enumUnmarshal(u, map[string]interface{}{
		"prometheus": &PrometheusMonitoring{},
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
