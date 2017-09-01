package cmd

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

var (
	JobSectionPush     string = "push"
	JobSectionPull     string = "pull"
	JobSectionPrune    string = "prune"
	JobSectionAutosnap string = "autosnap"
)

type Remote struct {
	Name      string
	Transport Transport
}

type Transport interface {
	Connect(rpcLog Logger) (rpc.RPCClient, error)
}
type LocalTransport struct {
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
	jobName           string // for use with jobrun package
	To                *Remote
	Filter            zfs.DatasetFilter
	InitialReplPolicy InitialReplPolicy
	RepeatStrategy    jobrun.RepeatStrategy
}
type Pull struct {
	jobName           string // for use with jobrun package
	From              *Remote
	Mapping           DatasetMapFilter
	InitialReplPolicy InitialReplPolicy
	RepeatStrategy    jobrun.RepeatStrategy
}

type Prune struct {
	jobName         string // for use with jobrun package
	DatasetFilter   zfs.DatasetFilter
	SnapshotFilter  zfs.FilesystemVersionFilter
	RetentionPolicy *RetentionGrid // TODO abstract interface to support future policies?
	Repeat          jobrun.RepeatStrategy
}

type Autosnap struct {
	jobName       string // for use with jobrun package
	Prefix        string
	Interval      jobrun.RepeatStrategy
	DatasetFilter zfs.DatasetFilter
}

type Config struct {
	Remotes   map[string]*Remote
	Pushs     map[string]*Push            // job name -> job
	Pulls     map[string]*Pull            // job name -> job
	Sinks     map[string]DatasetMapFilter // client identity -> mapping
	PullACLs  map[string]DatasetMapFilter // client identity -> filter
	Prunes    map[string]*Prune           // job name -> job
	Autosnaps map[string]*Autosnap        // job name -> job
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
	if c.Remotes, err = parseRemotes(root["remotes"]); err != nil {
		return
	}

	remoteLookup := func(name string) (remote *Remote, err error) {
		remote = c.Remotes[name]
		if remote == nil {
			err = fmt.Errorf("remote '%s' not defined", name)
		}
		return
	}

	if c.Pushs, err = parsePushs(root["pushs"], remoteLookup); err != nil {
		return
	}
	if c.Pulls, err = parsePulls(root["pulls"], remoteLookup); err != nil {
		return
	}
	if c.Sinks, err = parseSinks(root["sinks"]); err != nil {
		return
	}
	if c.PullACLs, err = parsePullACLs(root["pull_acls"]); err != nil {
		return
	}
	if c.Prunes, err = parsePrunes(root["prune"]); err != nil {
		return
	}
	if c.Autosnaps, err = parseAutosnaps(root["autosnap"]); err != nil {
		return
	}

	return
}

func fullJobName(section, name string) (full string, err error) {
	if len(name) < 1 {
		err = fmt.Errorf("job name not set")
		return
	}
	full = fmt.Sprintf("%s.%s", section, name)
	return
}

func parseRemotes(v interface{}) (remotes map[string]*Remote, err error) {

	asMap := make(map[string]struct {
		Transport map[string]interface{}
	}, 0)
	if err = mapstructure.Decode(v, &asMap); err != nil {
		return
	}

	remotes = make(map[string]*Remote, len(asMap))
	for name, p := range asMap {

		if name == LOCAL_TRANSPORT_IDENTITY {
			err = errors.New(fmt.Sprintf("remote name '%s' reserved for local pulls", LOCAL_TRANSPORT_IDENTITY))
			return
		}

		var transport Transport
		if transport, err = parseTransport(p.Transport); err != nil {
			return
		}
		remotes[name] = &Remote{
			Name:      name,
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

type remoteLookup func(name string) (*Remote, error)

func parsePushs(v interface{}, rl remoteLookup) (p map[string]*Push, err error) {

	asMap := make(map[string]struct {
		To                string
		Filter            map[string]string
		InitialReplPolicy string
		Repeat            map[string]string
	}, 0)

	if err = mapstructure.Decode(v, &asMap); err != nil {
		return
	}

	p = make(map[string]*Push, len(asMap))

	for name, e := range asMap {

		var toRemote *Remote
		if toRemote, err = rl(e.To); err != nil {
			return
		}
		push := &Push{
			To: toRemote,
		}

		if push.jobName, err = fullJobName(JobSectionPush, name); err != nil {
			return
		}
		if push.Filter, err = parseDatasetMapFilter(e.Filter, true); err != nil {
			return
		}

		if push.InitialReplPolicy, err = parseInitialReplPolicy(e.InitialReplPolicy, DEFAULT_INITIAL_REPL_POLICY); err != nil {
			return
		}

		if push.RepeatStrategy, err = parseRepeatStrategy(e.Repeat); err != nil {
			return
		}

		p[name] = push
	}

	return
}

func parsePulls(v interface{}, rl remoteLookup) (p map[string]*Pull, err error) {

	asMap := make(map[string]struct {
		From              string
		Mapping           map[string]string
		InitialReplPolicy string
		Repeat            map[string]string
	}, 0)

	if err = mapstructure.Decode(v, &asMap); err != nil {
		return
	}

	p = make(map[string]*Pull, len(asMap))

	for name, e := range asMap {

		if len(e.From) < 1 {
			err = fmt.Errorf("source not set ('from' attribute is empty)")
			return
		}

		var fromRemote *Remote

		if e.From == LOCAL_TRANSPORT_IDENTITY {
			fromRemote = &Remote{
				Name:      LOCAL_TRANSPORT_IDENTITY,
				Transport: LocalTransport{},
			}
		} else {
			if fromRemote, err = rl(e.From); err != nil {
				return
			}
		}

		pull := &Pull{
			From: fromRemote,
		}
		if pull.jobName, err = fullJobName(JobSectionPull, name); err != nil {
			return
		}
		if pull.Mapping, err = parseDatasetMapFilter(e.Mapping, false); err != nil {
			return
		}
		if pull.InitialReplPolicy, err = parseInitialReplPolicy(e.InitialReplPolicy, DEFAULT_INITIAL_REPL_POLICY); err != nil {
			return
		}
		if pull.RepeatStrategy, err = parseRepeatStrategy(e.Repeat); err != nil {
			return
		}

		p[name] = pull
	}

	return
}

func parseInitialReplPolicy(v interface{}, defaultPolicy InitialReplPolicy) (p InitialReplPolicy, err error) {
	s, ok := v.(string)
	if !ok {
		goto err
	}

	switch {
	case s == "":
		p = defaultPolicy
	case s == "most_recent":
		p = InitialReplPolicyMostRecent
	case s == "all":
		p = InitialReplPolicyAll
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
		d, err := parseDuration(repeatStr)
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

func parseSinks(v interface{}) (m map[string]DatasetMapFilter, err error) {
	var asMap map[string]map[string]interface{}
	if err = mapstructure.Decode(v, &asMap); err != nil {
		return
	}

	m = make(map[string]DatasetMapFilter, len(asMap))

	for identity, entry := range asMap {
		parseSink := func() (mapping DatasetMapFilter, err error) {
			mappingMap, ok := entry["mapping"]
			if !ok {
				err = fmt.Errorf("no mapping specified")
				return
			}
			mapping, err = parseDatasetMapFilter(mappingMap, false)
			return
		}
		mapping, sinkErr := parseSink()
		if sinkErr != nil {
			err = fmt.Errorf("cannot parse sink for identity '%s': %s", identity, sinkErr)
			return
		}
		m[identity] = mapping
	}
	return
}

func parsePullACLs(v interface{}) (m map[string]DatasetMapFilter, err error) {
	var asMap map[string]map[string]interface{}
	if err = mapstructure.Decode(v, &asMap); err != nil {
		return
	}

	m = make(map[string]DatasetMapFilter, len(asMap))

	for identity, entry := range asMap {
		parsePullACL := func() (filter DatasetMapFilter, err error) {
			filterMap, ok := entry["filter"]
			if !ok {
				err = fmt.Errorf("no filter specified")
				return
			}
			filter, err = parseDatasetMapFilter(filterMap, true)
			return
		}
		filter, filterErr := parsePullACL()
		if filterErr != nil {
			err = fmt.Errorf("cannot parse pull-ACL for identity '%s': %s", identity, filterErr)
			return
		}
		m[identity] = filter
	}
	return
}

func parseDatasetMapFilter(mi interface{}, filterOnly bool) (f DatasetMapFilter, err error) {

	var m map[string]string
	if err = mapstructure.Decode(mi, &m); err != nil {
		err = fmt.Errorf("maps / filters must be specified as map[string]string: %s", err)
		return
	}

	f = NewDatasetMapFilter(len(m), filterOnly)
	for pathPattern, mapping := range m {
		if err = f.Add(pathPattern, mapping); err != nil {
			err = fmt.Errorf("invalid mapping entry ['%s':'%s']: %s", pathPattern, mapping, err)
			return
		}
	}
	return
}

func (t SSHTransport) Connect(rpcLog Logger) (r rpc.RPCClient, err error) {
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
	client := rpc.NewClient(stream)
	return client, nil
}

func (t LocalTransport) Connect(rpcLog Logger) (r rpc.RPCClient, err error) {
	local := rpc.NewLocalRPC()
	handler := Handler{
		Logger: log,
		// Allow access to any dataset since we control what mapping
		// is passed to the pull routine.
		// All local datasets will be passed to its Map() function,
		// but only those for which a mapping exists will actually be pulled.
		// We can pay this small performance penalty for now.
		PullACL: localPullACL{},
	}
	registerEndpoints(local, handler)
	return local, nil
}

func parsePrunes(m interface{}) (rets map[string]*Prune, err error) {

	asList := make(map[string]map[string]interface{}, 0)
	if err = mapstructure.Decode(m, &asList); err != nil {
		return
	}

	rets = make(map[string]*Prune, len(asList))

	for name, e := range asList {
		var prune *Prune
		if prune, err = parsePrune(e, name); err != nil {
			err = fmt.Errorf("cannot parse prune job %s: %s", name, err)
			return
		}
		rets[name] = prune
	}

	return
}

func parsePrune(e map[string]interface{}, name string) (prune *Prune, err error) {

	// Only support grid policy for now
	policyName, ok := e["policy"]
	if !ok || policyName != "grid" {
		err = fmt.Errorf("prune job with unimplemented policy '%s'", policyName)
		return
	}

	var i struct {
		Grid           string
		DatasetFilter  map[string]string `mapstructure:"dataset_filter"`
		SnapshotFilter map[string]string `mapstructure:"snapshot_filter"`
		Repeat         map[string]string
	}

	if err = mapstructure.Decode(e, &i); err != nil {
		return
	}

	prune = &Prune{}

	if prune.jobName, err = fullJobName(JobSectionPrune, name); err != nil {
		return
	}

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
	if prune.DatasetFilter, err = parseDatasetMapFilter(i.DatasetFilter, true); err != nil {
		err = fmt.Errorf("cannot parse dataset filter: %s", err)
		return
	}
	if prune.SnapshotFilter, err = parseSnapshotFilter(i.SnapshotFilter); err != nil {
		err = fmt.Errorf("cannot parse snapshot filter: %s", err)
		return
	}

	// Parse repeat strategy
	if prune.Repeat, err = parseRepeatStrategy(i.Repeat); err != nil {
		err = fmt.Errorf("cannot parse repeat strategy: %s", err)
		return
	}

	return
}

var durationStringRegex *regexp.Regexp = regexp.MustCompile(`^\s*(\d+)\s*(s|m|h|d|w)\s*$`)

func parseDuration(e string) (d time.Duration, err error) {
	comps := durationStringRegex.FindStringSubmatch(e)
	if len(comps) != 3 {
		err = fmt.Errorf("does not match regex: %s %#v", e, comps)
		return
	}

	durationFactor, err := strconv.ParseInt(comps[1], 10, 64)
	if err != nil {
		return
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

var retentionStringIntervalRegex *regexp.Regexp = regexp.MustCompile(`^\s*(\d+)\s*x\s*([^\(]+)\s*(\((.*)\))?\s*$`)

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

	duration, err := parseDuration(comps[2])
	if err != nil {
		return nil, err
	}

	keepCount := 1
	if comps[3] != "" {
		// Decompose key=value, comma separated
		// For now, only keep_count is supported
		re := regexp.MustCompile(`^\s*keep=(.+)\s*$`)
		res := re.FindStringSubmatch(comps[4])
		if res == nil || len(res) != 2 {
			err = fmt.Errorf("interval parameter contains unknown parameters")
			return
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
			Length:    duration,
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

func parseAutosnaps(m interface{}) (snaps map[string]*Autosnap, err error) {

	asMap := make(map[string]interface{}, 0)
	if err = mapstructure.Decode(m, &asMap); err != nil {
		return
	}

	snaps = make(map[string]*Autosnap, len(asMap))

	for name, e := range asMap {
		var snap *Autosnap
		if snap, err = parseAutosnap(e, name); err != nil {
			err = fmt.Errorf("cannot parse autonsap job %s: %s", name, err)
			return
		}
		snaps[name] = snap
	}

	return

}

func parseAutosnap(m interface{}, name string) (a *Autosnap, err error) {

	var i struct {
		Prefix        string
		Interval      string
		DatasetFilter map[string]string `mapstructure:"dataset_filter"`
	}

	if err = mapstructure.Decode(m, &i); err != nil {
		err = fmt.Errorf("structure unfit: %s", err)
		return
	}

	a = &Autosnap{}

	if a.jobName, err = fullJobName(JobSectionAutosnap, name); err != nil {
		return
	}

	if len(i.Prefix) < 1 {
		err = fmt.Errorf("prefix must not be empty")
		return
	}
	a.Prefix = i.Prefix

	var interval time.Duration
	if interval, err = parseDuration(i.Interval); err != nil {
		err = fmt.Errorf("cannot parse interval: %s", err)
		return
	}
	a.Interval = &jobrun.PeriodicRepeatStrategy{interval}

	if len(i.DatasetFilter) == 0 {
		err = fmt.Errorf("dataset_filter not specified")
		return
	}
	if a.DatasetFilter, err = parseDatasetMapFilter(i.DatasetFilter, true); err != nil {
		err = fmt.Errorf("cannot parse dataset filter: %s", err)
	}

	return
}
