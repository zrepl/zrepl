package cmd

import (
	"fmt"
	"github.com/zrepl/zrepl/rpc"
	"github.com/zrepl/zrepl/zfs"
	"io"
)

type DatasetMapping interface {
	Map(source *zfs.DatasetPath) (target *zfs.DatasetPath, err error)
}

type Handler struct {
	Logger          Logger
	PullACL         zfs.DatasetFilter
	SinkMappingFunc func(clientIdentity string) (mapping DatasetMapping, err error)
}

func (h Handler) HandleFilesystemRequest(r rpc.FilesystemRequest) (roots []*zfs.DatasetPath, err error) {

	h.Logger.Printf("handling fsr: %#v", r)

	h.Logger.Printf("using PullACL: %#v", h.PullACL)

	if roots, err = zfs.ZFSListMapping(h.PullACL); err != nil {
		h.Logger.Printf("handle fsr err: %v\n", err)
		return
	}

	h.Logger.Printf("returning: %#v", roots)

	return
}

func (h Handler) HandleFilesystemVersionsRequest(r rpc.FilesystemVersionsRequest) (versions []zfs.FilesystemVersion, err error) {

	h.Logger.Printf("handling filesystem versions request: %#v", r)

	// allowed to request that?
	if h.pullACLCheck(r.Filesystem); err != nil {
		return
	}

	// find our versions
	if versions, err = zfs.ZFSListFilesystemVersions(r.Filesystem, nil); err != nil {
		h.Logger.Printf("our versions error: %#v\n", err)
		return
	}

	h.Logger.Printf("our versions: %#v\n", versions)
	return

}

func (h Handler) HandleInitialTransferRequest(r rpc.InitialTransferRequest) (stream io.Reader, err error) {

	h.Logger.Printf("handling initial transfer request: %#v", r)
	if err = h.pullACLCheck(r.Filesystem); err != nil {
		return
	}

	h.Logger.Printf("invoking zfs send")

	if stream, err = zfs.ZFSSend(r.Filesystem, &r.FilesystemVersion, nil); err != nil {
		h.Logger.Printf("error sending filesystem: %#v", err)
	}

	return

}

func (h Handler) HandleIncrementalTransferRequest(r rpc.IncrementalTransferRequest) (stream io.Reader, err error) {

	h.Logger.Printf("handling incremental transfer request: %#v", r)
	if err = h.pullACLCheck(r.Filesystem); err != nil {
		return
	}

	h.Logger.Printf("invoking zfs send")

	if stream, err = zfs.ZFSSend(r.Filesystem, &r.From, &r.To); err != nil {
		h.Logger.Printf("error sending filesystem: %#v", err)
	}

	return

}

func (h Handler) HandlePullMeRequest(r rpc.PullMeRequest, clientIdentity string, client rpc.RPCRequester) (err error) {

	// Check if we have a sink for this request
	// Use that mapping to do what happens in doPull

	h.Logger.Printf("handling PullMeRequest: %#v", r)

	var sinkMapping DatasetMapping
	sinkMapping, err = h.SinkMappingFunc(clientIdentity)
	if err != nil {
		h.Logger.Printf("no sink mapping for client identity '%s', denying PullMeRequest", clientIdentity)
		err = fmt.Errorf("no sink for client identity '%s'", clientIdentity)
		return
	}

	h.Logger.Printf("doing pull...")

	err = doPull(PullContext{
		Remote:            client,
		Log:               h.Logger,
		Mapping:           sinkMapping,
		InitialReplPolicy: r.InitialReplPolicy,
	})
	if err != nil {
		h.Logger.Printf("PullMeRequest failed with error: %s", err)
		return
	}

	h.Logger.Printf("finished handling PullMeRequest: %#v", r)

	return
}

func (h Handler) pullACLCheck(p *zfs.DatasetPath) (err error) {
	var allowed bool
	allowed, err = h.PullACL.Filter(p)
	if err != nil {
		err = fmt.Errorf("error evaluating ACL: %s", err)
		h.Logger.Printf(err.Error())
		return
	}
	if !allowed {
		err = fmt.Errorf("ACL prohibits access to %s", p.ToString())
		h.Logger.Printf(err.Error())
		return
	}
	return
}
