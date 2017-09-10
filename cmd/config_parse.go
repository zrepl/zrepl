package cmd

import (
	"io/ioutil"

	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	yaml "gopkg.in/yaml.v2"
	"fmt"
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

	var jm map[string]map[string]interface{}
	if err := mapstructure.Decode(i, &jm); err != nil {
		return nil, errors.Wrap(err, "config must be a dict with job name as key and jobs as values")
	}

	c = &Config{
		Jobs: make(map[string]Job, len(jm)),
	}

	for name := range jm {

		c.Jobs[name], err = parseJob(name, jm[name])
		if err != nil {
			err = errors.Wrapf(err, "cannot parse job '%s'", name)
			return nil, err
		}

	}

	return c, nil

}

func parseJob(name string, i map[string]interface{}) (j Job, err error) {

	jobtype_i, ok := i["type"]
	if !ok {
		err = errors.New("must have field 'type'")
		return nil, err
	}
	jobtype_str, ok := jobtype_i.(string)
	if !ok {
		err = errors.New("'type' field must have type string")
		return nil, err
	}

	switch jobtype_str {
	case "pull":
		return parsePullJob(name, i)
	case "source":
		return parseSourceJob(name, i)
	case "local":
		return parseLocalJob(name, i)
	default:
		return nil, errors.Errorf("unknown job type '%s'", jobtype_str)
	}

	panic("implementation error")
	return nil, nil

}

func parseConnect(i map[string]interface{}) (c RPCConnecter, err error) {
	type_i, ok := i["type"]
	if !ok {
		err = errors.New("must have field 'type'")
		return
	}
	type_str, ok := type_i.(string)
	if !ok {
		err = errors.New("'type' field must have type string")
		return nil, err
	}

	switch type_str {
	case "ssh+stdinserver":
		return parseSSHStdinserverConnecter(i)
	default:
		return nil, errors.Errorf("unknown connection type '%s'", type_str)
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

	policyName, ok := v["policy"]
	if !ok {
		err = errors.Errorf("policy name not specified")
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

	t, ok := v["type"]
	if !ok {
		err = errors.Errorf("must specify 'type' field")
		return
	}
	s, ok := t.(string)
	if !ok {
		err = errors.Errorf("'type' must be a string")
		return
	}

	switch s{
	case "stdinserver":
		return parseStdinserverListenerFactory(v)
	default:
		err = errors.Errorf("unknown type '%s'", s)
		return
	}

	panic("implementation error")
	return

}
