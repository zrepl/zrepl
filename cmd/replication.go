package cmd

import (
	"fmt"
	"io"

	"bytes"
	"encoding/json"
	"github.com/zrepl/zrepl/rpc"
	"github.com/zrepl/zrepl/zfs"
)

type localPullACL struct{}

func (a localPullACL) Filter(p *zfs.DatasetPath) (pass bool, err error) {
	return true, nil
}

const DEFAULT_INITIAL_REPL_POLICY = InitialReplPolicyMostRecent

type InitialReplPolicy string

const (
	InitialReplPolicyMostRecent InitialReplPolicy = "most_recent"
	InitialReplPolicyAll        InitialReplPolicy = "all"
)

type Puller struct {
	task              *Task
	Remote            rpc.RPCClient
	Mapping           DatasetMapping
	InitialReplPolicy InitialReplPolicy
}

type remoteLocalMapping struct {
	Remote *zfs.DatasetPath
	Local  *zfs.DatasetPath
}

func (p *Puller) getRemoteFilesystems() (rfs []*zfs.DatasetPath, ok bool) {
	p.task.Enter("fetch_remote_fs_list")
	defer p.task.Finish()

	fsr := FilesystemRequest{}
	if err := p.Remote.Call("FilesystemRequest", &fsr, &rfs); err != nil {
		p.task.Log().WithError(err).Error("cannot fetch remote filesystem list")
		return nil, false
	}
	return rfs, true
}

func (p *Puller) buildReplMapping(remoteFilesystems []*zfs.DatasetPath) (replMapping map[string]remoteLocalMapping, ok bool) {
	p.task.Enter("build_repl_mapping")
	defer p.task.Finish()

	replMapping = make(map[string]remoteLocalMapping, len(remoteFilesystems))
	for fs := range remoteFilesystems {
		var err error
		var localFs *zfs.DatasetPath
		localFs, err = p.Mapping.Map(remoteFilesystems[fs])
		if err != nil {
			err := fmt.Errorf("error mapping %s: %s", remoteFilesystems[fs], err)
			p.task.Log().WithError(err).WithField(logMapFromField, remoteFilesystems[fs]).Error("cannot map")
			return nil, false
		}
		if localFs == nil {
			continue
		}
		p.task.Log().WithField(logMapFromField, remoteFilesystems[fs].ToString()).
			WithField(logMapToField, localFs.ToString()).Debug("mapping")
		m := remoteLocalMapping{remoteFilesystems[fs], localFs}
		replMapping[m.Local.ToString()] = m
	}
	return replMapping, true
}

// returns true if the receiving filesystem (local side) exists and can have child filesystems
func (p *Puller) replFilesystem(m remoteLocalMapping, localFilesystemState map[string]zfs.FilesystemState) (localExists bool) {

	p.task.Enter("repl_fs")
	defer p.task.Finish()
	var err error
	remote := p.Remote

	log := p.task.Log().
		WithField(logMapToField, m.Remote.ToString()).
		WithField(logMapFromField, m.Local.ToString())

	log.Debug("examining local filesystem state")
	localState, localExists := localFilesystemState[m.Local.ToString()]
	var versions []zfs.FilesystemVersion
	switch {
	case !localExists:
		log.Info("local filesystem does not exist")
	case localState.Placeholder:
		log.Info("local filesystem is marked as placeholder")
	default:
		log.Debug("local filesystem exists")
		log.Debug("requesting local filesystem versions")
		if versions, err = zfs.ZFSListFilesystemVersions(m.Local, nil); err != nil {
			log.WithError(err).Error("cannot get local filesystem versions")
			return false
		}
	}

	log.Info("requesting remote filesystem versions")
	r := FilesystemVersionsRequest{
		Filesystem: m.Remote,
	}
	var theirVersions []zfs.FilesystemVersion
	if err = remote.Call("FilesystemVersionsRequest", &r, &theirVersions); err != nil {
		log.WithError(err).Error("cannot get remote filesystem versions")
		log.Warn("stopping replication for all filesystems mapped as children of receiving filesystem")
		return false
	}

	log.Debug("computing diff between remote and local filesystem versions")
	diff := zfs.MakeFilesystemDiff(versions, theirVersions)
	log.WithField("diff", diff).Debug("diff between local and remote filesystem")

	if localState.Placeholder && diff.Conflict != zfs.ConflictAllRight {
		panic("internal inconsistency: local placeholder implies ConflictAllRight")
	}

	switch diff.Conflict {
	case zfs.ConflictAllRight:

		log.WithField("replication_policy", p.InitialReplPolicy).Info("performing initial sync, following policy")

		if p.InitialReplPolicy != InitialReplPolicyMostRecent {
			panic(fmt.Sprintf("policy '%s' not implemented", p.InitialReplPolicy))
		}

		snapsOnly := make([]zfs.FilesystemVersion, 0, len(diff.MRCAPathRight))
		for s := range diff.MRCAPathRight {
			if diff.MRCAPathRight[s].Type == zfs.Snapshot {
				snapsOnly = append(snapsOnly, diff.MRCAPathRight[s])
			}
		}

		if len(snapsOnly) < 1 {
			log.Warn("cannot perform initial sync: no remote snapshots")
			return false
		}

		r := InitialTransferRequest{
			Filesystem:        m.Remote,
			FilesystemVersion: snapsOnly[len(snapsOnly)-1],
		}

		log.WithField("version", r.FilesystemVersion).Debug("requesting snapshot stream")

		var stream io.Reader

		if err = remote.Call("InitialTransferRequest", &r, &stream); err != nil {
			log.WithError(err).Error("cannot request initial transfer")
			return false
		}
		log.Debug("received initial transfer request response")

		log.Debug("invoke zfs receive")
		recvArgs := []string{"-u"}
		if localState.Placeholder {
			log.Info("receive with forced rollback to replace placeholder filesystem")
			recvArgs = append(recvArgs, "-F")
		}
		progressStream := p.task.ProgressUpdater(stream)
		if err = zfs.ZFSRecv(m.Local, progressStream, recvArgs...); err != nil {
			log.WithError(err).Error("cannot receive stream")
			return false
		}
		log.Info("finished receiving stream") // TODO rx delta

		// TODO unify with recv path of ConflictIncremental
		log.Debug("configuring properties of received filesystem")
		if err = zfs.ZFSSet(m.Local, "readonly", "on"); err != nil {
			log.WithError(err).Error("cannot set readonly property")
		}

		log.Info("finished initial transfer")
		return true

	case zfs.ConflictIncremental:

		if len(diff.IncrementalPath) < 2 {
			log.Info("remote and local are in sync")
			return true
		}

		log.Info("following incremental path from diff")
		for i := 0; i < len(diff.IncrementalPath)-1; i++ {

			from, to := diff.IncrementalPath[i], diff.IncrementalPath[i+1]

			log, _ := log.WithField(logIncFromField, from.Name).WithField(logIncToField, to.Name), 0

			log.Debug("requesting incremental snapshot stream")
			r := IncrementalTransferRequest{
				Filesystem: m.Remote,
				From:       from,
				To:         to,
			}
			var stream io.Reader
			if err = remote.Call("IncrementalTransferRequest", &r, &stream); err != nil {
				log.WithError(err).Error("cannot request incremental snapshot stream")
				return false
			}

			log.Debug("invoking zfs receive")
			progressStream := p.task.ProgressUpdater(stream)
			// TODO protect against malicious incremental stream
			if err = zfs.ZFSRecv(m.Local, progressStream); err != nil {
				log.WithError(err).Error("cannot receive stream")
				return false
			}
			log.Info("finished incremental transfer") // TODO increment rx

		}
		log.Info("finished following incremental path") // TODO path rx
		return true

	case zfs.ConflictNoCommonAncestor:
		fallthrough
	case zfs.ConflictDiverged:

		var jsonDiff bytes.Buffer
		if err := json.NewEncoder(&jsonDiff).Encode(diff); err != nil {
			log.WithError(err).Error("cannot JSON-encode diff")
			return false
		}

		var problem, resolution string

		switch diff.Conflict {
		case zfs.ConflictNoCommonAncestor:
			problem = "remote and local filesystem have snapshots, but no common one"
			resolution = "perform manual establish a common snapshot history"
		case zfs.ConflictDiverged:
			problem = "remote and local filesystem share a history but have diverged"
			resolution = "perform manual replication or delete snapshots on the receiving" +
				"side  to establish an incremental replication parse"
		}

		log.WithField("diff", jsonDiff.String()).
			WithField("problem", problem).
			WithField("resolution", resolution).
			Error("manual conflict resolution required")

		return false

	}

	panic("should not be reached")
}

func (p *Puller) Pull() {
	p.task.Enter("run")
	defer p.task.Finish()

	p.task.Log().Info("request remote filesystem list")
	remoteFilesystems, ok := p.getRemoteFilesystems()
	if !ok {
		return
	}

	p.task.Log().Debug("map remote filesystems to local paths and determine order for per-filesystem sync")
	replMapping, ok := p.buildReplMapping(remoteFilesystems)
	if !ok {

	}

	p.task.Log().Debug("build cache for already present local filesystem state")
	p.task.Enter("cache_local_fs_state")
	localFilesystemState, err := zfs.ZFSListFilesystemState()
	p.task.Finish()
	if err != nil {
		p.task.Log().WithError(err).Error("cannot request local filesystem state")
		return
	}

	localTraversal := zfs.NewDatasetPathForest()
	for _, m := range replMapping {
		localTraversal.Add(m.Local)
	}

	p.task.Log().Info("start per-filesystem sync")
	localTraversal.WalkTopDown(func(v zfs.DatasetPathVisit) bool {

		p.task.Enter("tree_walk")
		defer p.task.Finish()

		log := p.task.Log().WithField(logFSField, v.Path.ToString())

		if v.FilledIn {
			if _, exists := localFilesystemState[v.Path.ToString()]; exists {
				// No need to verify if this is a placeholder or not. It is sufficient
				// to know we can add child filesystems to it
				return true
			}
			log.Debug("create placeholder filesystem")
			p.task.Enter("create_placeholder")
			err = zfs.ZFSCreatePlaceholderFilesystem(v.Path)
			p.task.Finish()
			if err != nil {
				log.Error("cannot create placeholder filesystem")
				return false
			}
			return true
		}

		m, ok := replMapping[v.Path.ToString()]
		if !ok {
			panic("internal inconsistency: replMapping should contain mapping for any path that was not filled in by WalkTopDown()")
		}

		return p.replFilesystem(m, localFilesystemState)
	})

	return

}
