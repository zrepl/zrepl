package main

import (
	"io"
	"github.com/zrepl/zrepl/zfs"
	"github.com/zrepl/zrepl/model"
	"github.com/zrepl/zrepl/rpc"
)
type Handler struct {}

func (h Handler) HandleFilesystemRequest(r rpc.FilesystemRequest) (roots []model.Filesystem, err error) {

	roots = make([]model.Filesystem, 0, 10)

	for _, root := range r.Roots {
		var zfsRoot model.Filesystem
		if zfsRoot, err = zfs.FilesystemsAtRoot(root); err != nil {
			return
		}
		roots = append(roots, zfsRoot)
	}

	return
}

func (h Handler) HandleInitialTransferRequest(r rpc.InitialTransferRequest) (io.Reader, error) {
	// TODO ACL
	return zfs.InitialSend(r.Snapshot)
}

func (h Handler) HandleIncrementalTransferRequest(r rpc.IncrementalTransferRequest) (io.Reader, error) {
	// TODO ACL
	return zfs.IncrementalSend(r.FromSnapshot, r.ToSnapshot)
}
