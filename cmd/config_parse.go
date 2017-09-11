package cmd

import (
	"io/ioutil"

	"fmt"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	yaml "gopkg.in/yaml.v2"
)

func ParseConfig(path string) (config *Config, err error) {

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
	err = mapstructure.Decode(asMap.Global, &c.Global)
	if err != nil {
		err = errors.Wrap(err, "cannot parse global section: %s")
		return
	}

	// Parse Jobs
	c.Jobs = make(map[string]Job, len(asMap.Jobs))
	for i := range asMap.Jobs {
		job, err := parseJob(asMap.Jobs[i])
		if err != nil {
			// Try to find its name
			namei, ok := asMap.Jobs[i]["name"]
			if !ok {
				namei = fmt.Sprintf("<no name, %i in list>", i)
			}
			err = errors.Wrapf(err, "cannot parse job '%v'", namei)
			return nil, err
		}
		c.Jobs[job.JobName()] = job
	}

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

func parseJob(i map[string]interface{}) (j Job, err error) {

	name, err := extractStringField(i, "name", true)
	if err != nil {
		return
	}

	jobtype, err := extractStringField(i, "type", true)
	if err != nil {
		return

	}

	switch jobtype {
	case "pull":
		return parsePullJob(name, i)
	case "source":
		return parseSourceJob(name, i)
	case "local":
		return parseLocalJob(name, i)
	default:
		return nil, errors.Errorf("unknown job type '%s'", jobtype)
	}

	panic("implementation error")
	return nil, nil

}

func parseConnect(i map[string]interface{}) (c RPCConnecter, err error) {

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

	panic("implementation error")
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

func parsePrunePolicy(v map[string]interface{}) (p PrunePolicy, err error) {

	policyName, err := extractStringField(v, "policy", true)
	if err != nil {
		return
	}

	switch policyName {
	case "grid":
		return parseGridPrunePolicy(v)
	default:
		err = errors.Errorf("unknown policy '%s'", policyName)
		return
	}

	panic("implementation error")
	return

}

func parseAuthenticatedChannelListenerFactory(v map[string]interface{}) (p AuthenticatedChannelListenerFactory, err error) {

	t, err := extractStringField(v, "type", true)
	if err != nil {
		return nil, err
	}

	switch t {
	case "stdinserver":
		return parseStdinserverListenerFactory(v)
	default:
		err = errors.Errorf("unknown type '%s'", t)
		return
	}

	panic("implementation error")
	return

}
