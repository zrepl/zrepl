package cmd

import (
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/rpc"
	"github.com/zrepl/zrepl/util"
)

type PullJob struct {
	Name              string
	Connect           RWCConnecter
	Mapping           *DatasetMapFilter
	SnapshotFilter    *PrefixSnapshotFilter
	InitialReplPolicy InitialReplPolicy
	Prune             PrunePolicy
	Debug             JobDebugSettings
}

func parsePullJob(name string, i map[string]interface{}) (j *PullJob, err error) {

	var asMap struct {
		Connect           map[string]interface{}
		Mapping           map[string]string
		InitialReplPolicy string `mapstructure:"initial_repl_policy"`
		Prune             map[string]interface{}
		SnapshotPrefix    string `mapstructure:"snapshot_prefix"`
		Debug             map[string]interface{}
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

	if err = mapstructure.Decode(asMap.Debug, &j.Debug); err != nil {
		err = errors.Wrap(err, "cannot parse 'debug'")
		return
	}

	return
}

func (j *PullJob) JobName() string {
	return j.Name
}

func (j *PullJob) JobDo(log Logger) (err error) {

	rwc, err := j.Connect.Connect()
	if err != nil {
		log.Printf("error connect: %s", err)
		return err
	}

	rwc, err = util.NewReadWriteCloserLogger(rwc, j.Debug.Conn.ReadDump, j.Debug.Conn.WriteDump)
	if err != nil {
		return
	}

	client := rpc.NewClient(rwc)
	if j.Debug.RPC.Log {
		client.SetLogger(log, true)
	}

	defer closeRPCWithTimeout(log, client, time.Second*10, "")
	return doPull(PullContext{client, log, j.Mapping, j.InitialReplPolicy})
}
