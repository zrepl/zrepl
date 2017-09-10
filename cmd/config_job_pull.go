package cmd

import (
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
)

type PullJob struct {
	Name              string
	Connect           RPCConnecter
	Mapping           *DatasetMapFilter
	SnapshotFilter    *PrefixSnapshotFilter
	InitialReplPolicy InitialReplPolicy
	Prune             PrunePolicy
}

func parsePullJob(name string, i map[string]interface{}) (j *PullJob, err error) {

	var asMap struct {
		Connect           map[string]interface{}
		Mapping           map[string]string
		InitialReplPolicy string `mapstructure:"initial_repl_policy"`
		Prune             map[string]interface{}
		SnapshotPrefix    string `mapstructure:"snapshot_prefix"`
	}

	if err = mapstructure.Decode(i, &asMap); err != nil {
		err = errors.Wrap(err, "mapstructure error")
		return nil, err
	}

	j = &PullJob{Name: name}

	j.Connect, err = parseSSHStdinserverConnecter(asMap.Connect)
	if err != nil {
		err = errors.Wrap(err, "cannot parse 'connect'")
		return nil, err
	}

	j.Mapping, err = parseDatasetMapFilter(asMap.Mapping, false)
	if err != nil {
		err = errors.Wrap(err, "cannot parse 'mapping'")
		return nil, err
	}

	j.InitialReplPolicy, err = parseInitialReplPolicy(asMap.InitialReplPolicy, DEFAULT_INITIAL_REPL_POLICY)
	if err != nil {
		err = errors.Wrap(err, "cannot parse 'initial_repl_policy'")
		return
	}

	if j.SnapshotFilter, err = parsePrefixSnapshotFilter(asMap.SnapshotPrefix); err != nil {
		return
	}

	if j.Prune, err = parsePrunePolicy(asMap.Prune); err != nil {
		err = errors.Wrap(err, "cannot parse prune policy")
		return
	}

	return
}

func (j *PullJob) JobName() string {
	return j.Name
}

func (j *PullJob) JobDo(log Logger) (err error) {
	client, err := j.Connect.Connect()
	if err != nil {
		log.Printf("error connect: %s", err)
		return err
	}
	defer closeRPCWithTimeout(log, client, time.Second*10, "")
	return doPull(PullContext{client, log, j.Mapping, j.InitialReplPolicy})
}
