package config

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/zrepl/yaml-config"
	"io/ioutil"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"time"
)

type Config struct {
	Jobs   []JobEnum `yaml:"jobs"`
	Global *Global   `yaml:"global,optional,fromdefaults"`
}

func (c *Config) Job(name string) (*JobEnum, error) {
	for _, j := range c.Jobs {
		if j.Name() == name {
			return &j, nil
		}
	}
	return nil, fmt.Errorf("job %q not defined in config", name)
}

type JobEnum struct {
	Ret interface{}
}

func (j JobEnum) Name() string {
	var name string
	switch v := j.Ret.(type) {
	case *PushJob: name = v.Name
	case *SinkJob: name = v.Name
	case *PullJob: name = v.Name
	case *SourceJob: name = v.Name
	default:
		panic(fmt.Sprintf("unknownn job type %T", v))
	}
	return name
}

type ActiveJob struct {
	Type         string                `yaml:"type"`
	Name         string                `yaml:"name"`
	Connect     ConnectEnum     `yaml:"connect"`
	Pruning      PruningSenderReceiver `yaml:"pruning"`
	Debug        JobDebugSettings      `yaml:"debug,optional"`
}

type PassiveJob struct {
	Type        string           `yaml:"type"`
	Name        string           `yaml:"name"`
	Serve       ServeEnum `yaml:"serve"`
	Debug       JobDebugSettings `yaml:"debug,optional"`
}

type PushJob struct {
	ActiveJob `yaml:",inline"`
	Snapshotting SnapshottingEnum          `yaml:"snapshotting"`
	Filesystems FilesystemsFilter `yaml:"filesystems"`
}

type PullJob struct {
	ActiveJob `yaml:",inline"`
	RootFS    string        `yaml:"root_fs"`
	Interval  time.Duration `yaml:"interval,positive"`
}

type SinkJob struct {
	PassiveJob `yaml:",inline"`
	RootFS     string `yaml:"root_fs"`
}

type SourceJob struct {
	PassiveJob `yaml:",inline"`
	Snapshotting SnapshottingEnum      `yaml:"snapshotting"`
	Filesystems FilesystemsFilter `yaml:"filesystems"`
}

type FilesystemsFilter map[string]bool

type SnapshottingEnum struct {
	Ret interface{}
}

type SnapshottingPeriodic struct {
	Type string		`yaml:"type"`
	Prefix string	`yaml:"prefix"`
	Interval time.Duration `yaml:"interval,positive"`
}

type SnapshottingManual struct {
	Type string `yaml:"type"`
}

type PruningSenderReceiver struct {
	KeepSender   []PruningEnum `yaml:"keep_sender"`
	KeepReceiver []PruningEnum `yaml:"keep_receiver"`
}

type PruningLocal struct {
	Keep []PruningEnum `yaml:"keep"`
}

type LoggingOutletEnumList []LoggingOutletEnum

func (l *LoggingOutletEnumList) SetDefault() {
	def := `
type: "stdout"
time: true
level: "warn"
format: "human"
`
	s := &StdoutLoggingOutlet{}
	err := yaml.UnmarshalStrict([]byte(def), &s)
	if err != nil {
		panic(err)
	}
	*l = []LoggingOutletEnum{LoggingOutletEnum{Ret: s}}
}

var _ yaml.Defaulter = &LoggingOutletEnumList{}

type Global struct {
	Logging    *LoggingOutletEnumList `yaml:"logging,optional,fromdefaults"`
	Monitoring []MonitoringEnum       `yaml:"monitoring,optional"`
	Control    *GlobalControl         `yaml:"control,optional,fromdefaults"`
	Serve      *GlobalServe           `yaml:"serve,optional,fromdefaults"`
	RPC        *RPCConfig             `yaml:"rpc,optional,fromdefaults"`
}

func Default(i interface{}) {
	v := reflect.ValueOf(i)
	if v.Kind() != reflect.Ptr {
		panic(v)
	}
	y := `{}`
	err := yaml.Unmarshal([]byte(y), v.Interface())
	if err != nil {
		panic(err)
	}
}

type RPCConfig struct {
	Timeout             time.Duration `yaml:"timeout,optional,positive,default=10s"`
	TxChunkSize         uint32        `yaml:"tx_chunk_size,optional,default=32768"`
	RxStructuredMaxLen  uint32        `yaml:"rx_structured_max,optional,default=16777216"`
	RxStreamChunkMaxLen uint32        `yaml:"rx_stream_chunk_max,optional,default=16777216"`
	RxHeaderMaxLen      uint32        `yaml:"rx_header_max,optional,default=40960"`
	SendHeartbeatInterval             time.Duration `yaml:"send_heartbeat_interval,optional,positive,default=5s"`

}

type ConnectEnum struct {
	Ret interface{}
}

type ConnectCommon struct {
	Type string     `yaml:"type"`
	RPC  *RPCConfig `yaml:"rpc,optional"`
}

type TCPConnect struct {
	ConnectCommon `yaml:",inline"`
	Address       string        `yaml:"address"`
	DialTimeout   time.Duration `yaml:"dial_timeout,positive,default=10s"`
}

type TLSConnect struct {
	ConnectCommon `yaml:",inline"`
	Address       string        `yaml:"address"`
	Ca            string        `yaml:"ca"`
	Cert          string        `yaml:"cert"`
	Key           string        `yaml:"key"`
	ServerCN      string        `yaml:"server_cn"`
	DialTimeout   time.Duration `yaml:"dial_timeout,positive,default=10s"`
}

type SSHStdinserverConnect struct {
	ConnectCommon        `yaml:",inline"`
	Host                 string        `yaml:"host"`
	User                 string        `yaml:"user"`
	Port                 uint16        `yaml:"port"`
	IdentityFile         string        `yaml:"identity_file"`
	TransportOpenCommand []string      `yaml:"transport_open_command,optional"` //TODO unused
	SSHCommand           string        `yaml:"ssh_command,optional"`            //TODO unused
	Options              []string      `yaml:"options,optional"`
	DialTimeout          time.Duration `yaml:"dial_timeout,positive,default=10s"`
}

type LocalConnect struct {
	ConnectCommon `yaml:",inline"`
	ListenerName string `yaml:"listener_name"`
	ClientIdentity string `yaml:"client_identity"`
}

type ServeEnum struct {
	Ret interface{}
}

type ServeCommon struct {
	Type string     `yaml:"type"`
	RPC  *RPCConfig `yaml:"rpc,optional"`
}

type TCPServe struct {
	ServeCommon `yaml:",inline"`
	Listen      string            `yaml:"listen"`
	Clients     map[string]string `yaml:"clients"`
}

type TLSServe struct {
	ServeCommon      `yaml:",inline"`
	Listen           string        `yaml:"listen"`
	Ca               string        `yaml:"ca"`
	Cert             string        `yaml:"cert"`
	Key              string        `yaml:"key"`
	ClientCNs        []string      `yaml:"client_cns"`
	HandshakeTimeout time.Duration `yaml:"handshake_timeout,positive,default=10s"`
}

type StdinserverServer struct {
	ServeCommon    `yaml:",inline"`
	ClientIdentities []string `yaml:"client_identities"`
}

type LocalServe struct {
	ServeCommon `yaml:",inline"`
	ListenerName string `yaml:"listener_name"`
}

type PruningEnum struct {
	Ret interface{}
}

type PruneKeepNotReplicated struct {
	Type string `yaml:"type"`
	KeepSnapshotAtCursor bool `yaml:"keep_snapshot_at_cursor,optional,default=true"`
}

type PruneKeepLastN struct {
	Type  string `yaml:"type"`
	Count int    `yaml:"count"`
}

type PruneKeepRegex struct { // FIXME rename to KeepRegex
	Type  string `yaml:"type"`
	Regex string `yaml:"regex"`
	Negate bool   `yaml:"negate,optional,default=false"`
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
	Time                bool `yaml:"time,default=true"`
	Color               bool `yaml:"color,default=true"`
}

type SyslogLoggingOutlet struct {
	LoggingOutletCommon `yaml:",inline"`
	Facility            string `yaml:"facility,optional,default=local0"`
	RetryInterval       time.Duration `yaml:"retry_interval,positive,default=10s"`
}

type TCPLoggingOutlet struct {
	LoggingOutletCommon `yaml:",inline"`
	Address             string               `yaml:"address"`
	Net                 string               `yaml:"net,default=tcp"`
	RetryInterval       time.Duration        `yaml:"retry_interval,positive,default=10s"`
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
	StdinServer *GlobalStdinServer `yaml:"stdinserver,optional,fromdefaults"`
}

type GlobalStdinServer struct {
	SockDir string `yaml:"sockdir,default=/var/run/zrepl/stdinserver"`
}

type JobDebugSettings struct {
	Conn *struct {
		ReadDump  string `yaml:"read_dump"`
		WriteDump string `yaml:"write_dump"`
	} `yaml:"conn,optional"`
	RPCLog bool `yaml:"rpc_log,optional,default=false"`
}

func enumUnmarshal(u func(interface{}, bool) error, types map[string]interface{}) (interface{}, error) {
	var in struct {
		Type string
	}
	if err := u(&in, true); err != nil {
		return nil, err
	}
	if in.Type == "" {
		return nil, &yaml.TypeError{Errors: []string{"must specify type"}}
	}

	v, ok := types[in.Type]
	if !ok {
		return nil, &yaml.TypeError{Errors: []string{fmt.Sprintf("invalid type name %q", in.Type)}}
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
	})
	return
}

func (t *ConnectEnum) UnmarshalYAML(u func(interface{}, bool) error) (err error) {
	t.Ret, err = enumUnmarshal(u, map[string]interface{}{
		"tcp":             &TCPConnect{},
		"tls":             &TLSConnect{},
		"ssh+stdinserver": &SSHStdinserverConnect{},
		"local": 		   &LocalConnect{},
	})
	return
}

func (t *ServeEnum) UnmarshalYAML(u func(interface{}, bool) error) (err error) {
	t.Ret, err = enumUnmarshal(u, map[string]interface{}{
		"tcp":         &TCPServe{},
		"tls":         &TLSServe{},
		"stdinserver": &StdinserverServer{},
		"local"      : &LocalServe{},
	})
	return
}

func (t *PruningEnum) UnmarshalYAML(u func(interface{}, bool) error) (err error) {
	t.Ret, err = enumUnmarshal(u, map[string]interface{}{
		"not_replicated": &PruneKeepNotReplicated{},
		"last_n":         &PruneKeepLastN{},
		"grid":           &PruneGrid{},
		"regex":          &PruneKeepRegex{},
	})
	return
}

func (t *SnapshottingEnum) UnmarshalYAML(u func(interface{}, bool) error) (err error) {
	t.Ret, err = enumUnmarshal(u, map[string]interface{}{
		"periodic": &SnapshottingPeriodic{},
		"manual": &SnapshottingManual{},
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

func ParseConfig(path string) (i *Config, err error) {

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

	return ParseConfigBytes(bytes)
}

func ParseConfigBytes(bytes []byte) (*Config, error) {
	var c *Config
	if err := yaml.UnmarshalStrict(bytes, &c); err != nil {
		return nil, err
	}
	if c == nil {
		return nil, fmt.Errorf("config is empty or only consists of comments")
	}
	return c, nil
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
