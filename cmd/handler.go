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

func (h Handler) HandleResumeTransferRequest(r rpc.ResumeTransferRequest) (stream io.Reader, err error) {
	// decode receive_resume_token: zfs send -nvP <token>
	// A) use exit code to determine if could send (exit != 0 means could not send)
	// B) check ACL if the filesystem in toname field (it is a snapshot so strip its snapshot name first)
	//    is allowed for the given client
	/*

			# zfs send -nvt will print nvlist contents on ZoL and FreeBSD, should be good enough
			# will print regardless of whether it can send or not -> good for us
			# need clean way to extract that...
				# expect 'resume token contents:\nnvlist version: 0\n'
				# parse everything after that is tab-indented as key value pairs separated by = sign
				# => return dict

			zfs send -nv -t 1-c6491acbe-c8-789c636064000310a500c4ec50360710e72765a526973030b0419460caa7a515a79630c001489e0d493ea9b224b5182451885d7f497e7a69660a0343f79b1b9a8a2b3db65b20c97382e5f312735319188a4bf28b12d353f5931293b34b0b8af5ab8a520b72f4d3f2f31d8a53f35220660300c1091dbe
		resume token contents:
		nvlist version: 0
		        object = 0x6
		        offset = 0x0
		        bytes = 0x7100
		        toguid = 0xb748a92129d8ec8b
		        toname = storage/backups/zrepl/foo@send
		cannot resume send: 'storage/backups/zrepl/foo@send' used in the initial send no longer exists

		zfs send -nvt 1-ebbbbea7e-f0-789c636064000310a501c49c50360710a715e5e7a69766a6304041f79b1b9a8a2b3db62b00d9ec48eaf293b252934b181858a0ea30e4d3d28a534b18e00024cf86249f5459925a0ca44fc861d75f920f71c59c5fdf6f7b3eea32b24092e704cbe725e6a632301497e41725a6a7ea27252667971614eb5715a516e4e8a7e5e73b14a7e6a51881cd06005a222749
		resume token contents:
		nvlist version: 0
		        fromguid = 0xb748a92129d8ec8b #NOTE the fromguid field which is only in this one, so don't hardcode
		        object = 0x4
		        offset = 0x0
		        bytes = 0x1ec8
		        toguid = 0x328ae249dbf7fa9c
		        toname = storage/backups/zrepl/foo@send2
		send from storage/backups/zrepl/foo@send to storage/backups/zrepl/foo@send2 estimated size is 1.02M

	*/
	panic("not implemented")
	return nil, nil
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
