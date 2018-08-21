package cmd

import (
	"io/ioutil"

	"fmt"
	yaml "github.com/go-yaml/yaml"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"os"
	"regexp"
	"strconv"
	"time"
	"github.com/problame/go-streamrpc"
	"github.com/zrepl/zrepl/cmd/endpoint"
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

	cj, err := NewControlJob(JobNameControl, jpc.Global.Control.Sockpath)
	if err != nil {
		err = errors.Wrap(err, "cannot create control job")
		return
	}
	c.Jobs[JobNameControl] = cj

	return c, nil

}

func extractStringField(i map[string]interface{}, key string, notempty bool) (field string, err error) {
	vi, ok := i[key]
	if !ok {
		err = errors.Errorf("must have field '%s'", key)
		return "", err
	}
	field, ok = vi.(string)
	if !ok {
		err = errors.Errorf("'%s' field must have type string", key)
		return "", err
	}
	if notempty && len(field) <= 0 {
		err = errors.Errorf("'%s' field must not be empty", key)
		return "", err
	}
	return
}

type JobParsingContext struct {
	ConfigParsingContext
}

func parseJob(c JobParsingContext, i map[string]interface{}) (j Job, err error) {

	name, err := extractStringField(i, "name", true)
	if err != nil {
		return nil, err
	}

	for _, r := range ReservedJobNames {
		if name == r {
			err = errors.Errorf("job name '%s' is reserved", name)
			return nil, err
		}
	}

	jobtypeStr, err := extractStringField(i, "type", true)
	if err != nil {
		return nil, err
	}

	jobtype, err := ParseUserJobType(jobtypeStr)
	if err != nil {
		return nil, err
	}

	switch jobtype {
	case JobTypePull:
		return parsePullJob(c, name, i)
	case JobTypeSource:
		return parseSourceJob(c, name, i)
	case JobTypeLocal:
		return parseLocalJob(c, name, i)
	case JobTypePrometheus:
		return parsePrometheusJob(c, name, i)
	default:
		panic(fmt.Sprintf("implementation error: unknown job type %s", jobtype))
	}

}

func parseConnect(i map[string]interface{}) (c streamrpc.Connecter, err error) {

	t, err := extractStringField(i, "type", true)
	if err != nil {
		return nil, err
	}

	switch t {
	case "ssh+stdinserver":
		return parseSSHStdinserverConnecter(i)
	default:
		return nil, errors.Errorf("unknown connection type '%s'", t)
	}

}

func parseInitialReplPolicy(v interface{}, defaultPolicy endpoint.InitialReplPolicy) (p endpoint.InitialReplPolicy, err error) {
	s, ok := v.(string)
	if !ok {
		goto err
	}

	switch {
	case s == "":
		p = defaultPolicy
	case s == "most_recent":
		p = endpoint.InitialReplPolicyMostRecent
	case s == "all":
		p = endpoint.InitialReplPolicyAll
	default:
		goto err
	}

	return

err:
	err = errors.New(fmt.Sprintf("expected InitialReplPolicy, got %#v", v))
	return
}

func parsePrunePolicy(v map[string]interface{}, willSeeBookmarks bool) (p PrunePolicy, err error) {

	policyName, err := extractStringField(v, "policy", true)
	if err != nil {
		return
	}

	switch policyName {
	case "grid":
		return parseGridPrunePolicy(v, willSeeBookmarks)
	case "noprune":
		return NoPrunePolicy{}, nil
	default:
		err = errors.Errorf("unknown policy '%s'", policyName)
		return
	}
}

func parseAuthenticatedChannelListenerFactory(c JobParsingContext, v map[string]interface{}) (p ListenerFactory, err error) {

	t, err := extractStringField(v, "type", true)
	if err != nil {
		return nil, err
	}

	switch t {
	case "stdinserver":
		return parseStdinserverListenerFactory(c, v)
	default:
		err = errors.Errorf("unknown type '%s'", t)
		return
	}

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
