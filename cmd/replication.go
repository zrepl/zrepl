package main

import (
	"fmt"
	"github.com/zrepl/zrepl/rpc"
	"github.com/zrepl/zrepl/zfs"
	"io"
	"time"
)

func closeRPCWithTimeout(log Logger, remote rpc.RPCRequester, timeout time.Duration, goodbye string) {
	log.Printf("closing rpc connection")

	ch := make(chan error)
	go func() {
		ch <- remote.CloseRequest(rpc.CloseRequest{goodbye})
	}()

	var err error
	select {
	case <-time.After(timeout):
		err = fmt.Errorf("timeout exceeded (%s)", timeout)
	case closeRequestErr := <-ch:
		err = closeRequestErr
	}

	if err != nil {
		log.Printf("error closing connection: %s", err)
		err = remote.ForceClose()
		if err != nil {
			log.Printf("error force-closing connection: %s", err)
		}
	}
	return
}

type PullContext struct {
	Remote            rpc.RPCRequester
	Log               Logger
	Mapping           zfs.DatasetMapping
	InitialReplPolicy rpc.InitialReplPolicy
}

func doPull(pull PullContext) (err error) {

	remote := pull.Remote
	log := pull.Log

	fsr := rpc.FilesystemRequest{
		Direction: rpc.DirectionPull,
	}
	var remoteFilesystems []zfs.DatasetPath
	if remoteFilesystems, err = remote.FilesystemRequest(fsr); err != nil {
		return
	}

	type RemoteLocalMapping struct {
		Remote      zfs.DatasetPath
		Local       zfs.DatasetPath
		LocalExists bool
	}
	replMapping := make(map[string]RemoteLocalMapping, len(remoteFilesystems))
	localTraversal := zfs.NewDatasetPathForest()
	localExists, err := zfs.ZFSListFilesystemExists()
	if err != nil {
		log.Printf("cannot get local filesystems map: %s", err)
		return err
	}

	{

		log.Printf("mapping using %#v\n", pull.Mapping)
		for fs := range remoteFilesystems {
			var err error
			var localFs zfs.DatasetPath
			localFs, err = pull.Mapping.Map(remoteFilesystems[fs])
			if err != nil {
				if err != zfs.NoMatchError {
					log.Printf("error mapping %s: %#v\n", remoteFilesystems[fs], err)
					return err
				}
				continue
			}
			m := RemoteLocalMapping{remoteFilesystems[fs], localFs, localExists(localFs)}
			replMapping[m.Local.ToString()] = m
			localTraversal.Add(m.Local)
		}

	}

	log.Printf("remoteFilesystems: %#v\nreplMapping: %#v\n", remoteFilesystems, replMapping)

	// per fs sync, assume sorted in top-down order TODO

	localTraversal.WalkTopDown(func(v zfs.DatasetPathVisit) bool {

		if v.FilledIn {
			if localExists(v.Path) {
				return true
			}
			log.Printf("aborting, don't know how to create fill-in dataset %s", v.Path)
			err = fmt.Errorf("aborting, don't know how to create fill-in dataset: %s", v.Path)
			return false
		}

		m, ok := replMapping[v.Path.ToString()]
		if !ok {
			panic("internal inconsistency: replMapping should contain mapping for any path that was not filled in by WalkTopDown()")
		}

		log := func(format string, args ...interface{}) {
			log.Printf("[%s => %s]: %s", m.Remote.ToString(), m.Local.ToString(), fmt.Sprintf(format, args...))
		}

		log("mapping: %#v\n", m)

		var versions []zfs.FilesystemVersion
		if m.LocalExists {
			if versions, err = zfs.ZFSListFilesystemVersions(m.Local); err != nil {
				log("cannot get filesystem versions, stopping...: %v\n", m.Local.ToString(), m, err)
				return false
			}
		}

		var theirVersions []zfs.FilesystemVersion
		theirVersions, err = remote.FilesystemVersionsRequest(rpc.FilesystemVersionsRequest{
			Filesystem: m.Remote,
		})
		if err != nil {
			log("cannot fetch remote filesystem versions, stopping: %s", err)
			return false
		}

		diff := zfs.MakeFilesystemDiff(versions, theirVersions)
		log("diff: %#v\n", diff)

		if diff.IncrementalPath == nil {
			log("performing initial sync, following policy: %#v", pull.InitialReplPolicy)

			if pull.InitialReplPolicy != rpc.InitialReplPolicyMostRecent {
				panic(fmt.Sprintf("policy %#v not implemented", pull.InitialReplPolicy))
			}

			snapsOnly := make([]zfs.FilesystemVersion, 0, len(diff.MRCAPathRight))
			for s := range diff.MRCAPathRight {
				if diff.MRCAPathRight[s].Type == zfs.Snapshot {
					snapsOnly = append(snapsOnly, diff.MRCAPathRight[s])
				}
			}

			if len(snapsOnly) < 1 {
				log("cannot perform initial sync: no remote snapshots. stopping...")
				return false
			}

			r := rpc.InitialTransferRequest{
				Filesystem:        m.Remote,
				FilesystemVersion: snapsOnly[len(snapsOnly)-1],
			}

			log("requesting initial transfer")

			var stream io.Reader
			if stream, err = remote.InitialTransferRequest(r); err != nil {
				log("error initial transfer request, stopping...: %s", err)
				return false
			}

			log("received initial transfer request response. zfs recv...")

			if err = zfs.ZFSRecv(m.Local, stream, "-u"); err != nil {
				log("error receiving stream, stopping...: %s", err)
				return false
			}

			log("configuring properties of received filesystem")

			if err = zfs.ZFSSet(m.Local, "readonly", "on"); err != nil {

			}

			log("finished initial transfer")

		} else if len(diff.IncrementalPath) < 2 {
			log("remote and local are in sync")
		} else {

			log("incremental transfers using path: %#v", diff.IncrementalPath)

			for i := 0; i < len(diff.IncrementalPath)-1; i++ {

				from, to := diff.IncrementalPath[i], diff.IncrementalPath[i+1]

				log := func(format string, args ...interface{}) {
					log("[%s => %s]: %s", from.Name, to.Name, fmt.Sprintf(format, args...))
				}

				r := rpc.IncrementalTransferRequest{
					Filesystem: m.Remote,
					From:       from,
					To:         to,
				}
				log("requesting incremental transfer: %#v", r)

				var stream io.Reader
				if stream, err = remote.IncrementalTransferRequest(r); err != nil {
					log("error requesting incremental transfer, stopping...: %s", err.Error())
					return false
				}

				log("receving incremental transfer")

				if err = zfs.ZFSRecv(m.Local, stream); err != nil {
					log("error receiving stream, stopping...: %s", err)
					return false
				}

				log("finished incremental transfer")

			}

			log("finished incremental transfer path")

		}

		return true

	})

	return

}
