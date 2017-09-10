package cmd

import (
	"time"

	"github.com/zrepl/zrepl/rpc"
	mapstructure "github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
)

type SourceJob struct {
	Name           string
	Serve          AuthenticatedChannelListenerFactory
	Datasets       *DatasetMapFilter
	SnapshotFilter  *PrefixSnapshotFilter
	Interval       time.Duration
	Prune          PrunePolicy
}

func parseSourceJob(name string, i map[string]interface{}) (j *SourceJob, err error) {

	var asMap struct {
		Serve          map[string]interface{}
		Datasets       map[string]string
		SnapshotPrefix string `mapstructure:"snapshot_prefix"`
		Interval       string
		Prune          map[string]interface{}
	}

	if err = mapstructure.Decode(i, &asMap); err != nil {
		err = errors.Wrap(err, "mapstructure error")
		return nil, err
	}

	j = &SourceJob{Name: name}

	if j.Serve, err = parseAuthenticatedChannelListenerFactory(asMap.Serve); err != nil {
		return
	}

	if j.Datasets, err = parseDatasetMapFilter(asMap.Datasets, true); err != nil {
		return
	}

	if j.SnapshotFilter, err = parsePrefixSnapshotFilter(asMap.SnapshotPrefix); err != nil {
		return
	}

	if j.Interval, err = time.ParseDuration(asMap.Interval); err != nil {
		err = errors.Wrap(err, "cannot parse 'interval'")
		return
	}

	if j.Prune, err = parsePrunePolicy(asMap.Prune); err != nil {
		return
	}

	return
}

func (j *SourceJob) JobName() string {
	return j.Name
}

func (j *SourceJob) JobDo(log Logger) (err error) {

	// Setup automatic snapshotting

	listener, err := j.Serve.Listen()
	if err != nil {
		return err
	}

	for {

		// listener does auth for us
		rwc, err := listener.Accept()
		if err != nil {
			// if err != AuthError...
			panic(err) // TODO
		}

		// construct connection handler
		handler := Handler{
			Logger:  log,
			PullACL: j.Datasets,
			// TODO should set SinkMapping here? no, but check Handler impl
		}

		// handle connection
		server := rpc.NewServer(rwc)
		registerEndpoints(server, handler)
		if err = server.Serve(); err != nil {
			log.Printf("error serving connection: %s", err)
		}

		rwc.Close()
	}

}
