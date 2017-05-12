package main

import (
	"errors"
	"fmt"
	"github.com/jinzhu/copier"
	"github.com/mitchellh/mapstructure"
	"github.com/zrepl/zrepl/rpc"
	"github.com/zrepl/zrepl/sshbytestream"
	. "github.com/zrepl/zrepl/util"
	"github.com/zrepl/zrepl/zfs"
	yaml "gopkg.in/yaml.v2"
	"io"
	"io/ioutil"
	"strings"
)

const LOCAL_TRANSPORT_IDENTITY string = "local"

const DEFAULT_INITIAL_REPL_POLICY = InitialReplPolicyMostRecent

type Pool struct {
	Name      string
	Transport Transport
}

type Transport interface {
	Connect() (rpc.RPCRequester, error)
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

type InitialReplPolicy string

const (
	InitialReplPolicyMostRecent InitialReplPolicy = "most_recent"
	InitialReplPolicyAll        InitialReplPolicy = "all"
)

type Push struct {
	To                *Pool
	Datasets          []zfs.DatasetPath
	InitialReplPolicy InitialReplPolicy
}
type Pull struct {
	From              *Pool
	Mapping           zfs.DatasetMapping
	InitialReplPolicy InitialReplPolicy
}
type ClientMapping struct {
	From    string
	Mapping zfs.DatasetMapping
}

type Config struct {
	Pools    []Pool
	Pushs    []Push
	Pulls    []Pull
	Sinks    []ClientMapping
	PullACLs []ClientMapping
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

		if p.Name == LOCAL_TRANSPORT_IDENTITY {
			err = errors.New(fmt.Sprintf("pool name '%s' reserved for local pulls", LOCAL_TRANSPORT_IDENTITY))
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
		Datasets          []string
		InitialReplPolicy string
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
			To:       toPool,
			Datasets: make([]zfs.DatasetPath, len(e.Datasets)),
		}

		for i, ds := range e.Datasets {
			if push.Datasets[i], err = zfs.NewDatasetPath(ds); err != nil {
				return
			}
		}

		if push.InitialReplPolicy, err = parseInitialReplPolicy(e.InitialReplPolicy, DEFAULT_INITIAL_REPL_POLICY); err != nil {
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
	}, 0)

	if err = mapstructure.Decode(v, &asList); err != nil {
		return
	}

	p = make([]Pull, len(asList))

	for i, e := range asList {

		var fromPool *Pool

		if e.From == LOCAL_TRANSPORT_IDENTITY {
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
		if pull.InitialReplPolicy, err = parseInitialReplPolicy(e.InitialReplPolicy, DEFAULT_INITIAL_REPL_POLICY); err != nil {
			return
		}

		p[i] = pull
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

func (t SSHTransport) Connect() (r rpc.RPCRequester, err error) {
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
	return rpc.ConnectByteStreamRPC(stream)
}

func (t LocalTransport) Connect() (r rpc.RPCRequester, err error) {
	if t.Handler == nil {
		panic("local transport with uninitialized handler")
	}
	return rpc.ConnectLocalRPC(t.Handler), nil
}

func (t *LocalTransport) SetHandler(handler rpc.RPCHandler) {
	t.Handler = handler
}
