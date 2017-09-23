package cmd

import (
	"fmt"
	"io"
	"time"

	"bytes"
	"encoding/json"
	"github.com/zrepl/zrepl/rpc"
	"github.com/zrepl/zrepl/util"
	"github.com/zrepl/zrepl/zfs"
)

type localPullACL struct{}

func (a localPullACL) Filter(p *zfs.DatasetPath) (pass bool, err error) {
	return true, nil
}

const LOCAL_TRANSPORT_IDENTITY string = "local"

const DEFAULT_INITIAL_REPL_POLICY = InitialReplPolicyMostRecent

type InitialReplPolicy string

const (
	InitialReplPolicyMostRecent InitialReplPolicy = "most_recent"
	InitialReplPolicyAll        InitialReplPolicy = "all"
)

func closeRPCWithTimeout(log Logger, remote rpc.RPCClient, timeout time.Duration, goodbye string) {
	log.Info("closing rpc connection")

	ch := make(chan error)
	go func() {
		ch <- remote.Close()
		close(ch)
	}()

	var err error
	select {
	case <-time.After(timeout):
		err = fmt.Errorf("timeout exceeded (%s)", timeout)
	case closeRequestErr := <-ch:
		err = closeRequestErr
	}

	if err != nil {
		log.WithError(err).Error("error closing connection")
	}
	return
}

type PullContext struct {
	Remote            rpc.RPCClient
	Log               Logger
	Mapping           DatasetMapping
	InitialReplPolicy InitialReplPolicy
}

func doPull(pull PullContext) (err error) {

	remote := pull.Remote
	log := pull.Log

	log.Info("request remote filesystem list")
	fsr := FilesystemRequest{}
	var remoteFilesystems []*zfs.DatasetPath
	if err = remote.Call("FilesystemRequest", &fsr, &remoteFilesystems); err != nil {
		return
	}

	log.Debug("map remote filesystems to local paths and determine order for per-filesystem sync")
	type RemoteLocalMapping struct {
		Remote *zfs.DatasetPath
		Local  *zfs.DatasetPath
	}
	replMapping := make(map[string]RemoteLocalMapping, len(remoteFilesystems))
	localTraversal := zfs.NewDatasetPathForest()
	for fs := range remoteFilesystems {
		var err error
		var localFs *zfs.DatasetPath
		localFs, err = pull.Mapping.Map(remoteFilesystems[fs])
		if err != nil {
			err := fmt.Errorf("error mapping %s: %s", remoteFilesystems[fs], err)
			log.WithError(err).WithField(logMapFromField, remoteFilesystems[fs]).Error("cannot map")
			return err
		}
		if localFs == nil {
			continue
		}
		log.WithField(logMapFromField, remoteFilesystems[fs].ToString()).
			WithField(logMapToField, localFs.ToString()).Debug("mapping")
		m := RemoteLocalMapping{remoteFilesystems[fs], localFs}
		replMapping[m.Local.ToString()] = m
		localTraversal.Add(m.Local)
	}

	log.Debug("build cache for already present local filesystem state")
	localFilesystemState, err := zfs.ZFSListFilesystemState()
	if err != nil {
		log.WithError(err).Error("cannot request local filesystem state")
		return err
	}

	log.Info("start per-filesystem sync")
	localTraversal.WalkTopDown(func(v zfs.DatasetPathVisit) bool {

		log := log.WithField(logFSField, v.Path.ToString())

		if v.FilledIn {
			if _, exists := localFilesystemState[v.Path.ToString()]; exists {
				// No need to verify if this is a placeholder or not. It is sufficient
				// to know we can add child filesystems to it
				return true
			}
			log.Debug("create placeholder filesystem")
			err = zfs.ZFSCreatePlaceholderFilesystem(v.Path)
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

		log = log.WithField(logMapToField, m.Remote.ToString()).
			WithField(logMapFromField, m.Local.ToString())

		log.Debug("examing local filesystem state")
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

			log.WithField("replication_policy", pull.InitialReplPolicy).Info("performing initial sync, following policy")

			if pull.InitialReplPolicy != InitialReplPolicyMostRecent {
				panic(fmt.Sprintf("policy '%s' not implemented", pull.InitialReplPolicy))
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
			watcher := util.IOProgressWatcher{Reader: stream}
			watcher.KickOff(1*time.Second, func(p util.IOProgress) {
				log.WithField("total_rx", p.TotalRX).Info("progress on receive operation")
			})

			recvArgs := []string{"-u"}
			if localState.Placeholder {
				log.Info("receive with forced rollback to replace placeholder filesystem")
				recvArgs = append(recvArgs, "-F")
			}

			if err = zfs.ZFSRecv(m.Local, &watcher, recvArgs...); err != nil {
				log.WithError(err).Error("canot receive stream")
				return false
			}
			log.WithField("total_rx", watcher.Progress().TotalRX).
				Info("finished receiving stream")

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
			var pathRx uint64

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
				watcher := util.IOProgressWatcher{Reader: stream}
				watcher.KickOff(1*time.Second, func(p util.IOProgress) {
					log.WithField("total_rx", p.TotalRX).Info("progress on receive operation")
				})

				if err = zfs.ZFSRecv(m.Local, &watcher); err != nil {
					log.WithError(err).Error("cannot receive stream")
					return false
				}

				totalRx := watcher.Progress().TotalRX
				pathRx += totalRx
				log.WithField("total_rx", totalRx).Info("finished incremental transfer")

			}

			log.WithField("total_rx", pathRx).Info("finished following incremental path")
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

		panic("implementation error: this should not be reached")
		return false

	})

	return

}
