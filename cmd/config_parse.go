package cmd

import (
	"io/ioutil"

	"fmt"
	"github.com/go-yaml/yaml"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/problame/go-streamrpc"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/cmd/pruning/retentiongrid"
	"os"
)

var ConfigFileDefaultLocations []string = []string{
	"/etc/zrepl/zrepl.yml",
	"/usr/local/etc/zrepl/zrepl.yml",
}

const (
	JobNameControl string = "control"
)

var ReservedJobNames []string = []string{
	JobNameControl,
}

type ConfigParsingContext struct {
	Global *Global
}

func ParseConfig(path string) (config *Config, err error) {

	if path == "" {
		// Try default locations
		for _, l := range ConfigFileDefaultLocations {
			stat, err := os.Stat(l)
			if err != nil {
				continue
			}
			if !stat.Mode().IsRegular() {
				err = errors.Errorf("file at default location is not a regular file: %s", l)
				continue
			}
			path = l
			break
		}
	}

	var i interface{}

	var bytes []byte

	if bytes, err = ioutil.ReadFile(path); err != nil {
		err = errors.WithStack(err)
		return
	}

	if err = yaml.Unmarshal(bytes, &i); err != nil {
		err = errors.WithStack(err)
		return
	}

	return parseConfig(i)
}

func parseConfig(i interface{}) (c *Config, err error) {

	var asMap struct {
		Global map[string]interface{}
		Jobs   []map[string]interface{}
	}
	if err := mapstructure.Decode(i, &asMap); err != nil {
		return nil, errors.Wrap(err, "config root must be a dict")
	}

	c = &Config{}

	// Parse global with defaults
	c.Global.Serve.Stdinserver.SockDir = "/var/run/zrepl/stdinserver"
	c.Global.Control.Sockpath = "/var/run/zrepl/control"

	err = mapstructure.Decode(asMap.Global, &c.Global)
	if err != nil {
		err = errors.Wrap(err, "mapstructure error on 'global' section: %s")
		return
	}

	if c.Global.logging, err = parseLogging(asMap.Global["logging"]); err != nil {
		return nil, errors.Wrap(err, "cannot parse logging section")
	}

	cpc := ConfigParsingContext{&c.Global}
	jpc := JobParsingContext{cpc}
	c.Jobs = make(map[string]Job, len(asMap.Jobs))

	// FIXME internal jobs should not be mixed with user jobs
	// Monitoring Jobs
	var monJobs []map[string]interface{}
	if err := mapstructure.Decode(asMap.Global["monitoring"], &monJobs); err != nil {
		return nil, errors.Wrap(err, "cannot parse monitoring section")
	}
	for i, jc := range monJobs {
		if jc["name"] == "" || jc["name"] == nil {
			// FIXME internal jobs should not require a name...
			jc["name"] = fmt.Sprintf("prometheus-%d", i)
		}
		job, err := parseJob(jpc, jc)
		if err != nil {
			return nil, errors.Wrapf(err, "cannot parse monitoring job #%d", i)
		}
		if job.JobType() != JobTypePrometheus {
			return nil, errors.Errorf("monitoring job #%d has invalid job type", i)
		}
		c.Jobs[job.JobName()] = job
	}

	// Regular Jobs
	for i := range asMap.Jobs {
		job, err := parseJob(jpc, asMap.Jobs[i])
		if err != nil {
			// Try to find its name
			namei, ok := asMap.Jobs[i]["name"]
			if !ok {
				namei = fmt.Sprintf("<no name, entry #%d in list>", i)
			}
			err = errors.Wrapf(err, "cannot parse job '%v'", namei)
			return nil, err
		}
		jn := job.JobName()
		if _, ok := c.Jobs[jn]; ok {
			err = errors.Errorf("duplicate or invalid job name: %s", jn)
			return nil, err
		}
		c.Jobs[job.JobName()] = job
	}

	return c, nil

}

type JobParsingContext struct {
	ConfigParsingContext
}

func parseJob(c config.Global, in config.JobEnum) (j Job, err error) {

	switch v := in.Ret.(type) {
	case config.PullJob:
		return parsePullJob(c, v)
	case config.SourceJob:
		return parseSourceJob(c, v)
	case config.LocalJob:
		return parseLocalJob(c, v)
	default:
		panic(fmt.Sprintf("implementation error: unknown job type %s", v))
	}

}

func parseConnect(in config.ConnectEnum) (c streamrpc.Connecter, err error) {
	switch v := in.Ret.(type) {
	case config.SSHStdinserverConnect:
		return parseSSHStdinserverConnecter(v)
	case config.TCPConnect:
		return parseTCPConnecter(v)
	case config.TLSConnect:
		return parseTLSConnecter(v)
	default:
		panic(fmt.Sprintf("unknown connect type %v", v))
	}
}

func parsePruning(in []config.PruningEnum, willSeeBookmarks bool) (p Pruner, err error) {

	policies := make([]PrunePolicy, len(in))
	for i := range in {
		if policies[i], err = parseKeepRule(in[i]); err != nil {
			return nil, errors.Wrapf(err, "invalid keep rule #%d:", i)
		}
	}

}

func parseKeepRule(in config.PruningEnum) (p PrunePolicy, err error) {
	switch v := in.Ret.(type) {
	case config.PruneGrid:
		return retentiongrid.ParseGridPrunePolicy(v, willSeeBookmarks)
	//case config.PruneKeepLastN:
	//case config.PruneKeepPrefix:
	//case config.PruneKeepNotReplicated:
	default:
		panic(fmt.Sprintf("unknown keep rule type %v", v))
	}
}

func parseAuthenticatedChannelListenerFactory(c config.Global, in config.ServeEnum) (p ListenerFactory, err error) {

	switch v := in.Ret.(type) {
	case config.StdinserverServer:
		return parseStdinserverListenerFactory(c, v)
	case config.TCPServe:
		return parseTCPListenerFactory(c, v)
	case config.TLSServe:
		return parseTLSListenerFactory(c, v)
	default:
		panic(fmt.Sprintf("unknown listener type %v", v))
	}

}
