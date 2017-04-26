package main

import (
	yaml "gopkg.in/yaml.v2"
	"io/ioutil"
	"github.com/mitchellh/mapstructure"
	"github.com/zrepl/zrepl/zfs"
	"errors"
	"strings"
)

type Pool struct {
	Name 	string
	Url 	string
}
type Push struct {
	To 		 string
	Datasets []zfs.DatasetPath
}
type Pull struct {
	From     string
	Mapping  zfs.DatasetMapping
}
type Sink struct {
	From 	 string
	Mapping  zfs.DatasetMapping
}

type Config struct {
	Pools []Pool
	Pushs []Push
	Pulls []Pull
	Sinks []Sink
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
	if c.Pushs, err = parsePushs(root["pushs"]); err != nil {
		return
	}
	if c.Pulls, err = parsePulls(root["pulls"]); err != nil {
		return
	}
	if c.Sinks, err = parseSinks(root["sinks"]); err != nil {
		return
	}
	return
}

func parsePools(v interface{}) (p []Pool, err error) {
	p = make([]Pool, 0)
	err = mapstructure.Decode(v, &p)
	return
}

func parsePushs(v interface{}) (p []Push, err error) {

	asList := make([]struct{
		To 		 string
		Datasets []string
	}, 0)

	if err = mapstructure.Decode(v, &asList); err != nil {
		return
	}

	p = make([]Push, len(asList))

	for _, e := range asList {
		push := Push{
			To: e.To,
			Datasets: make([]zfs.DatasetPath, len(e.Datasets)),
		}

		for i, ds := range e.Datasets {
			if push.Datasets[i], err = zfs.NewDatasetPath(ds); err != nil {
				return
			}
		}

		p = append(p, push)
	}

	return
}

func parsePulls(v interface{}) (p []Pull, err error) {

	asList := make([]struct{
		From 	string
		Mapping map[string]string
	}, 0)


	if err = mapstructure.Decode(v, &asList); err != nil {
		return
	}

	p = make([]Pull, len(asList))

	for _, e := range asList {
		pull := Pull{
			From: e.From,
		}
		if pull.Mapping, err = parseComboMapping(e.Mapping); err != nil {
			return
		}
		p = append(p, pull)
	}

	return
}

func parseSinks(v interface{}) (s []Sink, err error) {

	var asList []interface{}
	var ok bool
	if asList, ok  = v.([]interface{}); !ok {
		return nil, errors.New("expected list")
	}

	s = make([]Sink, len(asList))

	for _, i := range asList {
		var sink Sink
		if sink, err = parseSink(i); err != nil {
			return
		}
		s = append(s, sink)
	}

	return
}

func parseSink(v interface{}) (s Sink, err error) {
	t := struct {
		From string
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

	c.Mappings = make([]zfs.DatasetMapping, len(m))

	for lhs,rhs := range m {

		if lhs[0] == '|' {

			if len(m) != 1 {
				err = errors.New("non-recursive mapping must be the only mapping for a sink")
			}

			m := zfs.DirectMapping{
				 Source: nil,
			}

			if m.Target, err = zfs.NewDatasetPath(rhs); err != nil {
				return
			}

			c.Mappings = append(c.Mappings, m)

		} else if lhs[0] == '*' {

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

		}

	}

	return

}
