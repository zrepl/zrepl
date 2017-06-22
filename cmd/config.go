package main

import (
	"errors"
	"fmt"
	"github.com/jinzhu/copier"
	"github.com/mitchellh/mapstructure"
	"github.com/zrepl/zrepl/jobrun"
	"github.com/zrepl/zrepl/rpc"
	"github.com/zrepl/zrepl/sshbytestream"
	. "github.com/zrepl/zrepl/util"
	"github.com/zrepl/zrepl/zfs"
	yaml "gopkg.in/yaml.v2"
	"io"
	"io/ioutil"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Pool struct {
	Name      string
	Transport Transport
}

type Transport interface {
	Connect(rpcLog Logger) (rpc.RPCRequester, error)
}
type LocalTransport struct {
	Handler rpc.RPCHandler
}
type SSHTransport struct {
	Host                 string
	User                 string
	Port                 uint16
	IdentityFile         string   `mapstructure:"identity_file"`
	TransportOpenCommand []string `mapstructure:"transport_open_command"`
	SSHCommand           string   `mapstructure:"ssh_command"`
	Options              []string
	ConnLogReadFile      string `mapstructure:"connlog_read_file"`
	ConnLogWriteFile     string `mapstructure:"connlog_write_file"`
}

type Push struct {
	To                *Pool
	Filter            zfs.DatasetMapping
	InitialReplPolicy rpc.InitialReplPolicy
	RepeatStrategy    jobrun.RepeatStrategy
}
type Pull struct {
	From              *Pool
	Mapping           zfs.DatasetMapping
	InitialReplPolicy rpc.InitialReplPolicy
	RepeatStrategy    jobrun.RepeatStrategy
}

type ClientMapping struct {
	From    string
	Mapping zfs.DatasetMapping
}

type Prune struct {
	Name            string
	DatasetFilter   zfs.DatasetMapping
	SnapshotFilter  zfs.FilesystemVersionFilter
	RetentionPolicy *RetentionGrid // TODO abstract interface to support future policies?
}

type Config struct {
	Pools    []Pool
	Pushs    []Push
	Pulls    []Pull
	Sinks    []ClientMapping
	PullACLs []ClientMapping
	Prunes   []Prune
}

func ParseConfig(path string) (config Config, err error) {

	c := make(map[string]interface{}, 0)

	var bytes []byte

	if bytes, err = ioutil.ReadFile(path); err != nil {
		return
	}

	if err = yaml.Unmarshal(bytes, &c); err != nil {
		return
	}

	return parseMain(c)
}

func parseMain(root map[string]interface{}) (c Config, err error) {
	if c.Pools, err = parsePools(root["pools"]); err != nil {
		return
	}

	poolLookup := func(name string) (*Pool, error) {
		for _, pool := range c.Pools {
			if pool.Name == name {
				return &pool, nil
			}
		}
		return nil, errors.New(fmt.Sprintf("pool '%s' not defined", name))
	}

	if c.Pushs, err = parsePushs(root["pushs"], poolLookup); err != nil {
		return
	}
	if c.Pulls, err = parsePulls(root["pulls"], poolLookup); err != nil {
		return
	}
	if c.Sinks, err = parseClientMappings(root["sinks"]); err != nil {
		return
	}
	if c.PullACLs, err = parseClientMappings(root["pull_acls"]); err != nil {
		return
	}
	if c.Prunes, err = parsePrunes(root["prune"]); err != nil {
		return
	}
	return
}

func parsePools(v interface{}) (pools []Pool, err error) {

	asList := make([]struct {
		Name      string
		Transport map[string]interface{}
	}, 0)
	if err = mapstructure.Decode(v, &asList); err != nil {
		return
	}

	pools = make([]Pool, len(asList))
	for i, p := range asList {

		if p.Name == rpc.LOCAL_TRANSPORT_IDENTITY {
			err = errors.New(fmt.Sprintf("pool name '%s' reserved for local pulls", rpc.LOCAL_TRANSPORT_IDENTITY))
			return
		}

		var transport Transport
		if transport, err = parseTransport(p.Transport); err != nil {
			return
		}
		pools[i] = Pool{
			Name:      p.Name,
			Transport: transport,
		}
	}

	return
}

func parseTransport(it map[string]interface{}) (t Transport, err error) {

	if len(it) != 1 {
		err = errors.New("ambiguous transport type")
		return
	}

	for key, val := range it {
		switch key {
		case "ssh":
			t := SSHTransport{}
			if err = mapstructure.Decode(val, &t); err != nil {
				err = errors.New(fmt.Sprintf("could not parse ssh transport: %s", err))
				return nil, err
			}
			return t, nil
		default:
			return nil, errors.New(fmt.Sprintf("unknown transport type '%s'\n", key))
		}
	}

	return // unreachable

}

type poolLookup func(name string) (*Pool, error)

func parsePushs(v interface{}, pl poolLookup) (p []Push, err error) {

	asList := make([]struct {
		To                string
		Filter            map[string]string
		InitialReplPolicy string
		Repeat            map[string]string
	}, 0)

	if err = mapstructure.Decode(v, &asList); err != nil {
		return
	}

	p = make([]Push, len(asList))

	for i, e := range asList {

		var toPool *Pool
		if toPool, err = pl(e.To); err != nil {
			return
		}
		push := Push{
			To: toPool,
		}

		if push.Filter, err = parseComboMapping(e.Filter); err != nil {
			return
		}

		if push.InitialReplPolicy, err = parseInitialReplPolicy(e.InitialReplPolicy, rpc.DEFAULT_INITIAL_REPL_POLICY); err != nil {
			return
		}

		if push.RepeatStrategy, err = parseRepeatStrategy(e.Repeat); err != nil {
			return
		}

		p[i] = push
	}

	return
}

func parsePulls(v interface{}, pl poolLookup) (p []Pull, err error) {

	asList := make([]struct {
		From              string
		Mapping           map[string]string
		InitialReplPolicy string
		Repeat            map[string]string
	}, 0)

	if err = mapstructure.Decode(v, &asList); err != nil {
		return
	}

	p = make([]Pull, len(asList))

	for i, e := range asList {

		var fromPool *Pool

		if e.From == rpc.LOCAL_TRANSPORT_IDENTITY {
			fromPool = &Pool{
				Name:      "local",
				Transport: LocalTransport{},
			}
		} else {
			if fromPool, err = pl(e.From); err != nil {
				return
			}
		}

		pull := Pull{
			From: fromPool,
		}
		if pull.Mapping, err = parseComboMapping(e.Mapping); err != nil {
			return
		}
		if pull.InitialReplPolicy, err = parseInitialReplPolicy(e.InitialReplPolicy, rpc.DEFAULT_INITIAL_REPL_POLICY); err != nil {
			return
		}
		if pull.RepeatStrategy, err = parseRepeatStrategy(e.Repeat); err != nil {
			return
		}

		p[i] = pull
	}

	return
}

func parseInitialReplPolicy(v interface{}, defaultPolicy rpc.InitialReplPolicy) (p rpc.InitialReplPolicy, err error) {
	s, ok := v.(string)
	if !ok {
		goto err
	}

	switch {
	case s == "":
		p = defaultPolicy
	case s == "most_recent":
		p = rpc.InitialReplPolicyMostRecent
	case s == "all":
		p = rpc.InitialReplPolicyAll
	default:
		goto err
	}

	return

err:
	err = errors.New(fmt.Sprintf("expected InitialReplPolicy, got %#v", v))
	return
}

func parseRepeatStrategy(r map[string]string) (s jobrun.RepeatStrategy, err error) {

	if r == nil {
		return jobrun.NoRepeatStrategy{}, nil
	}

	if repeatStr, ok := r["interval"]; ok {
		d, err := time.ParseDuration(repeatStr)
		if err != nil {
			return nil, err
		}
		s = &jobrun.PeriodicRepeatStrategy{d}
		return s, err
	} else {
		return nil, fmt.Errorf("attribute 'interval' not found but required in repeat specification")
	}

}

func expectList(v interface{}) (asList []interface{}, err error) {
	var ok bool
	if asList, ok = v.([]interface{}); !ok {
		err = errors.New("expected list")
	}
	return
}

func parseClientMappings(v interface{}) (cm []ClientMapping, err error) {

	var asList []interface{}
	if asList, err = expectList(v); err != nil {
		return
	}

	cm = make([]ClientMapping, len(asList))

	for i, e := range asList {
		var m ClientMapping
		if m, err = parseClientMapping(e); err != nil {
			return
		}
		cm[i] = m
	}
	return
}

func parseClientMapping(v interface{}) (s ClientMapping, err error) {
	t := struct {
		From    string
		Mapping map[string]string
	}{}
	if err = mapstructure.Decode(v, &t); err != nil {
		return
	}

	s.From = t.From
	s.Mapping, err = parseComboMapping(t.Mapping)
	return
}

func parseComboMapping(m map[string]string) (c zfs.ComboMapping, err error) {

	c.Mappings = make([]zfs.DatasetMapping, 0, len(m))

	for lhs, rhs := range m {

		if lhs == "*" && strings.HasPrefix(rhs, "!") {

			m := zfs.ExecMapping{}
			fields := strings.Fields(strings.TrimPrefix(rhs, "!"))
			if len(fields) < 1 {
				err = errors.New("ExecMapping without acceptor path")
				return
			}
			m.Name = fields[0]
			m.Args = fields[1:]

			c.Mappings = append(c.Mappings, m)

		} else if strings.HasSuffix(lhs, "*") {

			m := zfs.GlobMapping{}

			m.PrefixPath, err = zfs.NewDatasetPath(strings.TrimSuffix(lhs, "*"))
			if err != nil {
				return
			}

			if m.TargetRoot, err = zfs.NewDatasetPath(rhs); err != nil {
				return
			}

			c.Mappings = append(c.Mappings, m)

		} else {

			m := zfs.DirectMapping{}

			if lhs == "|" {
				m.Source = nil
			} else {
				if m.Source, err = zfs.NewDatasetPath(lhs); err != nil {
					return
				}
			}

			if m.Target, err = zfs.NewDatasetPath(rhs); err != nil {
				return
			}

			c.Mappings = append(c.Mappings, m)

		}

	}

	return

}

func (t SSHTransport) Connect(rpcLog Logger) (r rpc.RPCRequester, err error) {
	var stream io.ReadWriteCloser
	var rpcTransport sshbytestream.SSHTransport
	if err = copier.Copy(&rpcTransport, t); err != nil {
		return
	}
	if stream, err = sshbytestream.Outgoing(rpcTransport); err != nil {
		return
	}
	stream, err = NewReadWriteCloserLogger(stream, t.ConnLogReadFile, t.ConnLogWriteFile)
	if err != nil {
		return
	}
	return rpc.ConnectByteStreamRPC(stream, rpcLog)
}

func (t LocalTransport) Connect(rpcLog Logger) (r rpc.RPCRequester, err error) {
	if t.Handler == nil {
		panic("local transport with uninitialized handler")
	}
	return rpc.ConnectLocalRPC(t.Handler), nil
}

func (t *LocalTransport) SetHandler(handler rpc.RPCHandler) {
	t.Handler = handler
}

func parsePrunes(m interface{}) (rets []Prune, err error) {

	asList := make([]map[string]interface{}, 0)
	if err = mapstructure.Decode(m, &asList); err != nil {
		return
	}

	rets = make([]Prune, len(asList))

	for i, e := range asList {
		if rets[i], err = parsePrune(e); err != nil {
			err = fmt.Errorf("cannot parse prune job #%d: %s", i+1, err)
			return
		}
	}

	return
}

func parsePrune(e map[string]interface{}) (prune Prune, err error) {
	// Only support grid policy for now
	policyName, ok := e["policy"]
	if !ok || policyName != "grid" {
		err = fmt.Errorf("prune job with unimplemented policy '%s'", policyName)
		return
	}

	var i struct {
		Name           string
		Grid           string
		DatasetFilter  map[string]string `mapstructure:"dataset_filter"`
		SnapshotFilter map[string]string `mapstructure:"snapshot_filter"`
	}

	if err = mapstructure.Decode(e, &i); err != nil {
		return
	}

	prune.Name = i.Name

	// Parse grid policy
	intervals, err := parseRetentionGridIntervalsString(i.Grid)
	if err != nil {
		err = fmt.Errorf("cannot parse retention grid: %s", err)
		return
	}
	// Assert intervals are of increasing length (not necessarily required, but indicates config mistake)
	lastDuration := time.Duration(0)
	for i := range intervals {
		if intervals[i].Length < lastDuration {
			err = fmt.Errorf("retention grid interval length must be monotonically increasing:"+
				"interval %d is shorter than %d", i+1, i)
			return
		} else {
			lastDuration = intervals[i].Length
		}
	}
	prune.RetentionPolicy = NewRetentionGrid(intervals)

	// Parse filters
	if prune.DatasetFilter, err = parseComboMapping(i.DatasetFilter); err != nil {
		err = fmt.Errorf("cannot parse dataset filter: %s", err)
		return
	}
	if prune.SnapshotFilter, err = parseSnapshotFilter(i.SnapshotFilter); err != nil {
		err = fmt.Errorf("cannot parse snapshot filter: %s", err)
		return
	}

	return
}

var retentionStringIntervalRegex *regexp.Regexp = regexp.MustCompile(`^\s*(\d+)\s*x\s*(\d+)\s*(s|min|h|d|w|mon)\s*\s*(\((.*)\))?\s*$`)

func parseRetentionGridIntervalString(e string) (intervals []RetentionInterval, err error) {

	comps := retentionStringIntervalRegex.FindStringSubmatch(e)
	if comps == nil {
		err = fmt.Errorf("retention string does not match expected format")
		return
	}

	times, err := strconv.Atoi(comps[1])
	if err != nil {
		return nil, err
	} else if times <= 0 {
		return nil, fmt.Errorf("contains factor <= 0")
	}

	durationFactor, err := strconv.ParseInt(comps[2], 10, 64)
	if err != nil {
		return nil, err
	}

	var durationUnit time.Duration
	switch comps[3] {
	case "s":
		durationUnit = time.Second
	case "min":
		durationUnit = time.Minute
	case "h":
		durationUnit = time.Hour
	case "d":
		durationUnit = 24 * time.Hour
	case "w":
		durationUnit = 24 * 7 * time.Hour
	case "mon":
		durationUnit = 32 * 24 * 7 * time.Hour
	default:
		err = fmt.Errorf("contains unknown time unit '%s'", comps[3])
		return nil, err
	}

	keepCount := 1
	if comps[4] != "" {
		// Decompose key=value, comma separated
		// For now, only keep_count is supported
		re := regexp.MustCompile(`^\s*keep=(.+)\s*$`)
		res := re.FindStringSubmatch(comps[5])
		if res == nil || len(res) != 2 {
			err = fmt.Errorf("interval parameter contains unknown parameters")
		}
		if res[1] == "all" {
			keepCount = RetentionGridKeepCountAll
		} else {
			keepCount, err = strconv.Atoi(res[1])
			if err != nil {
				err = fmt.Errorf("cannot parse keep_count value")
				return
			}
		}
	}

	intervals = make([]RetentionInterval, times)
	for i := range intervals {
		intervals[i] = RetentionInterval{
			Length:    time.Duration(durationFactor) * durationUnit, // TODO is this conversion fututre-proof?
			KeepCount: keepCount,
		}
	}

	return

}

func parseRetentionGridIntervalsString(s string) (intervals []RetentionInterval, err error) {

	ges := strings.Split(s, "|")
	intervals = make([]RetentionInterval, 0, 7*len(ges))

	for intervalIdx, e := range ges {
		parsed, err := parseRetentionGridIntervalString(e)
		if err != nil {
			return nil, fmt.Errorf("cannot parse interval %d of %d: %s: %s", intervalIdx+1, len(ges), err, strings.TrimSpace(e))
		}
		intervals = append(intervals, parsed...)
	}

	return
}

type prefixSnapshotFilter struct {
	prefix string
}

func (f prefixSnapshotFilter) Filter(fsv zfs.FilesystemVersion) (accept bool, err error) {
	return fsv.Type == zfs.Snapshot && strings.HasPrefix(fsv.Name, f.prefix), nil
}

func parseSnapshotFilter(fm map[string]string) (snapFilter zfs.FilesystemVersionFilter, err error) {
	prefix, ok := fm["prefix"]
	if !ok {
		err = fmt.Errorf("unsupported snapshot filter")
		return
	}
	snapFilter = prefixSnapshotFilter{prefix}
	return
}
