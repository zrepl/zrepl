package config

import (
	"fmt"
	"io/ioutil"
	"log/syslog"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/zrepl/yaml-config"

	"github.com/zrepl/zrepl/util/datasizeunit"
	zfsprop "github.com/zrepl/zrepl/zfs/property"
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
	case *SnapJob:
		name = v.Name
	case *PushJob:
		name = v.Name
	case *SinkJob:
		name = v.Name
	case *PullJob:
		name = v.Name
	case *SourceJob:
		name = v.Name
	default:
		panic(fmt.Sprintf("unknown job type %T", v))
	}
	return name
}

type ActiveJob struct {
	Type        string                `yaml:"type"`
	Name        string                `yaml:"name"`
	Connect     ConnectEnum           `yaml:"connect"`
	Pruning     PruningSenderReceiver `yaml:"pruning"`
	Debug       JobDebugSettings      `yaml:"debug,optional"`
	Replication *Replication          `yaml:"replication,optional,fromdefaults"`
}

type PassiveJob struct {
	Type  string           `yaml:"type"`
	Name  string           `yaml:"name"`
	Serve ServeEnum        `yaml:"serve"`
	Debug JobDebugSettings `yaml:"debug,optional"`
}

type SnapJob struct {
	Type         string            `yaml:"type"`
	Name         string            `yaml:"name"`
	Pruning      PruningLocal      `yaml:"pruning"`
	Debug        JobDebugSettings  `yaml:"debug,optional"`
	Snapshotting SnapshottingEnum  `yaml:"snapshotting"`
	Filesystems  FilesystemsFilter `yaml:"filesystems"`
}

type SendOptions struct {
	Encrypted        bool `yaml:"encrypted,optional,default=false"`
	Raw              bool `yaml:"raw,optional,default=false"`
	SendProperties   bool `yaml:"send_properties,optional,default=false"`
	BackupProperties bool `yaml:"backup_properties,optional,default=false"`
	LargeBlocks      bool `yaml:"large_blocks,optional,default=false"`
	Compressed       bool `yaml:"compressed,optional,default=false"`
	EmbeddedData     bool `yaml:"embedded_data,optional,default=false"`
	Saved            bool `yaml:"saved,optional,default=false"`

	BandwidthLimit *BandwidthLimit `yaml:"bandwidth_limit,optional,fromdefaults"`
}

type RecvOptions struct {
	// Note: we cannot enforce encrypted recv as the ZFS cli doesn't provide a mechanism for it
	// Encrypted bool `yaml:"may_encrypted"`
	// Future:
	// Reencrypt bool `yaml:"reencrypt"`

	Properties *PropertyRecvOptions `yaml:"properties,fromdefaults"`

	BandwidthLimit *BandwidthLimit `yaml:"bandwidth_limit,optional,fromdefaults"`

	Placeholder *PlaceholderRecvOptions `yaml:"placeholder,fromdefaults"`
}

var _ yaml.Unmarshaler = &datasizeunit.Bits{}

type BandwidthLimit struct {
	Max            datasizeunit.Bits `yaml:"max,default=-1 B"`
	BucketCapacity datasizeunit.Bits `yaml:"bucket_capacity,default=128 KiB"`
}

type Replication struct {
	Protection  *ReplicationOptionsProtection  `yaml:"protection,optional,fromdefaults"`
	Concurrency *ReplicationOptionsConcurrency `yaml:"concurrency,optional,fromdefaults"`
}

type ReplicationOptionsProtection struct {
	Initial     string `yaml:"initial,optional,default=guarantee_resumability"`
	Incremental string `yaml:"incremental,optional,default=guarantee_resumability"`
}

type ReplicationOptionsConcurrency struct {
	Steps         int `yaml:"steps,optional,default=1"`
	SizeEstimates int `yaml:"size_estimates,optional,default=4"`
}

type PropertyRecvOptions struct {
	Inherit  []zfsprop.Property          `yaml:"inherit,optional"`
	Override map[zfsprop.Property]string `yaml:"override,optional"`
}

type PlaceholderRecvOptions struct {
	Encryption string `yaml:"encryption,default=unspecified"`
}

type PushJob struct {
	ActiveJob    `yaml:",inline"`
	Snapshotting SnapshottingEnum  `yaml:"snapshotting"`
	Filesystems  FilesystemsFilter `yaml:"filesystems"`
	Send         *SendOptions      `yaml:"send,fromdefaults,optional"`
}

func (j *PushJob) GetFilesystems() FilesystemsFilter { return j.Filesystems }
func (j *PushJob) GetSendOptions() *SendOptions      { return j.Send }

type PullJob struct {
	ActiveJob `yaml:",inline"`
	RootFS    string                   `yaml:"root_fs"`
	Interval  PositiveDurationOrManual `yaml:"interval"`
	Recv      *RecvOptions             `yaml:"recv,fromdefaults,optional"`
}

func (j *PullJob) GetRootFS() string             { return j.RootFS }
func (j *PullJob) GetAppendClientIdentity() bool { return false }
func (j *PullJob) GetRecvOptions() *RecvOptions  { return j.Recv }

type PositiveDurationOrManual struct {
	Interval time.Duration
	Manual   bool
}

var _ yaml.Unmarshaler = (*PositiveDurationOrManual)(nil)

func (i *PositiveDurationOrManual) UnmarshalYAML(u func(interface{}, bool) error) (err error) {
	var s string
	if err := u(&s, true); err != nil {
		return err
	}
	switch s {
	case "manual":
		i.Manual = true
		i.Interval = 0
	case "":
		return fmt.Errorf("value must not be empty")
	default:
		i.Manual = false
		i.Interval, err = time.ParseDuration(s)
		if err != nil {
			return err
		}
		if i.Interval <= 0 {
			return fmt.Errorf("value must be a positive duration, got %q", s)
		}
	}
	return nil
}

type SinkJob struct {
	PassiveJob `yaml:",inline"`
	RootFS     string       `yaml:"root_fs"`
	Recv       *RecvOptions `yaml:"recv,optional,fromdefaults"`
}

func (j *SinkJob) GetRootFS() string             { return j.RootFS }
func (j *SinkJob) GetAppendClientIdentity() bool { return true }
func (j *SinkJob) GetRecvOptions() *RecvOptions  { return j.Recv }

type SourceJob struct {
	PassiveJob   `yaml:",inline"`
	Snapshotting SnapshottingEnum  `yaml:"snapshotting"`
	Filesystems  FilesystemsFilter `yaml:"filesystems"`
	Send         *SendOptions      `yaml:"send,optional,fromdefaults"`
}

func (j *SourceJob) GetFilesystems() FilesystemsFilter { return j.Filesystems }
func (j *SourceJob) GetSendOptions() *SendOptions      { return j.Send }

type FilesystemsFilter map[string]bool

type SnapshottingEnum struct {
	Ret interface{}
}

type SnapshottingPeriodic struct {
	Type     string        `yaml:"type"`
	Prefix   string        `yaml:"prefix"`
	Interval time.Duration `yaml:"interval,positive"`
	Hooks    HookList      `yaml:"hooks,optional"`
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

type ConnectEnum struct {
	Ret interface{}
}

type ConnectCommon struct {
	Type string `yaml:"type"`
}

type TCPConnect struct {
	ConnectCommon `yaml:",inline"`
	Address       string        `yaml:"address,hostport"`
	DialTimeout   time.Duration `yaml:"dial_timeout,zeropositive,default=10s"`
}

type TLSConnect struct {
	ConnectCommon `yaml:",inline"`
	Address       string        `yaml:"address,hostport"`
	Ca            string        `yaml:"ca"`
	Cert          string        `yaml:"cert"`
	Key           string        `yaml:"key"`
	ServerCN      string        `yaml:"server_cn"`
	DialTimeout   time.Duration `yaml:"dial_timeout,zeropositive,default=10s"`
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
	DialTimeout          time.Duration `yaml:"dial_timeout,zeropositive,default=10s"`
}

type LocalConnect struct {
	ConnectCommon  `yaml:",inline"`
	ListenerName   string        `yaml:"listener_name"`
	ClientIdentity string        `yaml:"client_identity"`
	DialTimeout    time.Duration `yaml:"dial_timeout,zeropositive,default=2s"`
}

type ServeEnum struct {
	Ret interface{}
}

type ServeCommon struct {
	Type string `yaml:"type"`
}

type TCPServe struct {
	ServeCommon    `yaml:",inline"`
	Listen         string            `yaml:"listen,hostport"`
	ListenFreeBind bool              `yaml:"listen_freebind,default=false"`
	Clients        map[string]string `yaml:"clients"`
}

type TLSServe struct {
	ServeCommon      `yaml:",inline"`
	Listen           string        `yaml:"listen,hostport"`
	ListenFreeBind   bool          `yaml:"listen_freebind,default=false"`
	Ca               string        `yaml:"ca"`
	Cert             string        `yaml:"cert"`
	Key              string        `yaml:"key"`
	ClientCNs        []string      `yaml:"client_cns"`
	HandshakeTimeout time.Duration `yaml:"handshake_timeout,zeropositive,default=10s"`
}

type StdinserverServer struct {
	ServeCommon      `yaml:",inline"`
	ClientIdentities []string `yaml:"client_identities"`
}

type LocalServe struct {
	ServeCommon  `yaml:",inline"`
	ListenerName string `yaml:"listener_name"`
}

type PruningEnum struct {
	Ret interface{}
}

type PruneKeepNotReplicated struct {
	Type                 string `yaml:"type"`
	KeepSnapshotAtCursor bool   `yaml:"keep_snapshot_at_cursor,optional,default=true"`
}

type PruneKeepLastN struct {
	Type  string `yaml:"type"`
	Count int    `yaml:"count"`
	Regex string `yaml:"regex,optional"`
}

type PruneKeepRegex struct { // FIXME rename to KeepRegex
	Type   string `yaml:"type"`
	Regex  string `yaml:"regex"`
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
	Facility            *SyslogFacility `yaml:"facility,optional,fromdefaults"`
	RetryInterval       time.Duration   `yaml:"retry_interval,positive,default=10s"`
}

type TCPLoggingOutlet struct {
	LoggingOutletCommon `yaml:",inline"`
	Address             string               `yaml:"address,hostport"`
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
	Type           string `yaml:"type"`
	Listen         string `yaml:"listen,hostport"`
	ListenFreeBind bool   `yaml:"listen_freebind,default=false"`
}

type SyslogFacility syslog.Priority

func (f *SyslogFacility) SetDefault() {
	*f = SyslogFacility(syslog.LOG_LOCAL0)
}

var _ yaml.Defaulter = (*SyslogFacility)(nil)

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

type HookList []HookEnum

type HookEnum struct {
	Ret interface{}
}

type HookCommand struct {
	Path               string            `yaml:"path"`
	Timeout            time.Duration     `yaml:"timeout,optional,positive,default=30s"`
	Filesystems        FilesystemsFilter `yaml:"filesystems,optional,default={'<': true}"`
	HookSettingsCommon `yaml:",inline"`
}

type HookPostgresCheckpoint struct {
	HookSettingsCommon `yaml:",inline"`
	DSN                string            `yaml:"dsn"`
	Timeout            time.Duration     `yaml:"timeout,optional,positive,default=30s"`
	Filesystems        FilesystemsFilter `yaml:"filesystems"` // required, user should not CHECKPOINT for every FS
}

type HookMySQLLockTables struct {
	HookSettingsCommon `yaml:",inline"`
	DSN                string            `yaml:"dsn"`
	Timeout            time.Duration     `yaml:"timeout,optional,positive,default=30s"`
	Filesystems        FilesystemsFilter `yaml:"filesystems"`
}

type HookSettingsCommon struct {
	Type       string `yaml:"type"`
	ErrIsFatal bool   `yaml:"err_is_fatal,optional,default=false"`
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
		"snap":   &SnapJob{},
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
		"local":           &LocalConnect{},
	})
	return
}

func (t *ServeEnum) UnmarshalYAML(u func(interface{}, bool) error) (err error) {
	t.Ret, err = enumUnmarshal(u, map[string]interface{}{
		"tcp":         &TCPServe{},
		"tls":         &TLSServe{},
		"stdinserver": &StdinserverServer{},
		"local":       &LocalServe{},
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
		"manual":   &SnapshottingManual{},
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

func (t *SyslogFacility) UnmarshalYAML(u func(interface{}, bool) error) (err error) {
	var s string
	if err := u(&s, true); err != nil {
		return err
	}
	var level syslog.Priority
	switch s {
	case "kern":
		level = syslog.LOG_KERN
	case "user":
		level = syslog.LOG_USER
	case "mail":
		level = syslog.LOG_MAIL
	case "daemon":
		level = syslog.LOG_DAEMON
	case "auth":
		level = syslog.LOG_AUTH
	case "syslog":
		level = syslog.LOG_SYSLOG
	case "lpr":
		level = syslog.LOG_LPR
	case "news":
		level = syslog.LOG_NEWS
	case "uucp":
		level = syslog.LOG_UUCP
	case "cron":
		level = syslog.LOG_CRON
	case "authpriv":
		level = syslog.LOG_AUTHPRIV
	case "ftp":
		level = syslog.LOG_FTP
	case "local0":
		level = syslog.LOG_LOCAL0
	case "local1":
		level = syslog.LOG_LOCAL1
	case "local2":
		level = syslog.LOG_LOCAL2
	case "local3":
		level = syslog.LOG_LOCAL3
	case "local4":
		level = syslog.LOG_LOCAL4
	case "local5":
		level = syslog.LOG_LOCAL5
	case "local6":
		level = syslog.LOG_LOCAL6
	case "local7":
		level = syslog.LOG_LOCAL7
	default:
		return fmt.Errorf("invalid syslog level: %q", s)
	}
	*t = SyslogFacility(level)
	return nil
}

func (t *HookEnum) UnmarshalYAML(u func(interface{}, bool) error) (err error) {
	t.Ret, err = enumUnmarshal(u, map[string]interface{}{
		"command":             &HookCommand{},
		"postgres-checkpoint": &HookPostgresCheckpoint{},
		"mysql-lock-tables":   &HookMySQLLockTables{},
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

func parsePositiveDuration(e string) (d time.Duration, err error) {
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
