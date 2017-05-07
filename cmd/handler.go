package main

import (
	"github.com/zrepl/zrepl/rpc"
	"github.com/zrepl/zrepl/zfs"
	"io"
)

type Handler struct {
	PushMapping zfs.DatasetMapping
	PullMapping zfs.DatasetMapping
}

func (h Handler) HandleFilesystemRequest(r rpc.FilesystemRequest) (roots []zfs.DatasetPath, err error) {
	return
}

func (h Handler) HandleFilesystemVersionsRequest(r rpc.FilesystemVersionsRequest) (versions []zfs.FilesystemVersion, err error) {
	return
}

func (h Handler) HandleInitialTransferRequest(r rpc.InitialTransferRequest) (stream io.Reader, err error) {
	return
}

func (h Handler) HandleIncrementalTransferRequest(r rpc.IncrementalTransferRequest) (stream io.Reader, err error) {
	return
}
