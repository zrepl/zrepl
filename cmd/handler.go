package main

import (
	"github.com/zrepl/zrepl/rpc"
	"github.com/zrepl/zrepl/zfs"
	"io"
)

type Handler struct {
	Logger          Logger
	PullACL         zfs.DatasetMapping
}

func (h Handler) HandleFilesystemRequest(r rpc.FilesystemRequest) (roots []zfs.DatasetPath, err error) {

	h.Logger.Printf("handling fsr: %#v", r)

	if roots, err = zfs.ZFSListMapping(h.PullACL); err != nil {
		h.Logger.Printf("handle fsr err: %v\n", err)
		return
	}

	h.Logger.Printf("got filesystems: %#v", roots)

	return
}

func (h Handler) HandleFilesystemVersionsRequest(r rpc.FilesystemVersionsRequest) (versions []zfs.FilesystemVersion, err error) {

	// allowed to request that?
	if _, err = h.PullACL.Map(r.Filesystem); err != nil {
		h.Logger.Printf("filesystem: %#v\n", r.Filesystem)
		h.Logger.Printf("pull mapping: %#v\n", h.PullACL)
		h.Logger.Printf("allowed error: %#v\n", err)
		return
	}
	h.Logger.Printf("allowed: %#v\n", r.Filesystem)

	// find our versions
	if versions, err = zfs.ZFSListFilesystemVersions(r.Filesystem); err != nil {
		h.Logger.Printf("our versions error: %#v\n", err)
		return
	}

	h.Logger.Printf("our versions: %#v\n", versions)
	return

}

func (h Handler) HandleInitialTransferRequest(r rpc.InitialTransferRequest) (stream io.Reader, err error) {

	h.Logger.Printf("handling initial transfer request: %#v", r)
	// allowed to request that?
	if _, err = h.PullACL.Map(r.Filesystem); err != nil {
		h.Logger.Printf("initial transfer request acl errror: %#v", err)
		return
	}

	h.Logger.Printf("invoking zfs send")

	if stream, err = zfs.ZFSSend(r.Filesystem, &r.FilesystemVersion, nil); err != nil {
		h.Logger.Printf("error sending filesystem: %#v", err)
	}

	h.Logger.Printf("finished zfs send")

	return

}

func (h Handler) HandleIncrementalTransferRequest(r rpc.IncrementalTransferRequest) (stream io.Reader, err error) {

	h.Logger.Printf("handling incremental transfer request: %#v", r)
	// allowed to request that?
	if _, err = h.PullACL.Map(r.Filesystem); err != nil {
		h.Logger.Printf("incremental transfer request acl errror: %#v", err)
		return
	}

	h.Logger.Printf("invoking zfs send")

	if stream, err = zfs.ZFSSend(r.Filesystem, &r.From, &r.To); err != nil {
		h.Logger.Printf("error sending filesystem: %#v", err)
	}

	h.Logger.Printf("finished zfs send")

	return

}
