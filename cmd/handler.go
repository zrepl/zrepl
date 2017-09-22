package cmd

import (
	"fmt"
	"io"

	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/rpc"
	"github.com/zrepl/zrepl/zfs"
)

type DatasetMapping interface {
	Map(source *zfs.DatasetPath) (target *zfs.DatasetPath, err error)
}

type FilesystemRequest struct {
	Roots []string // may be nil, indicating interest in all filesystems
}

type FilesystemVersionsRequest struct {
	Filesystem *zfs.DatasetPath
}

type InitialTransferRequest struct {
	Filesystem        *zfs.DatasetPath
	FilesystemVersion zfs.FilesystemVersion
}

type IncrementalTransferRequest struct {
	Filesystem *zfs.DatasetPath
	From       zfs.FilesystemVersion
	To         zfs.FilesystemVersion
}

type Handler struct {
	logger Logger
	dsf    zfs.DatasetFilter
	fsvf   zfs.FilesystemVersionFilter
}

func NewHandler(logger Logger, dsfilter zfs.DatasetFilter, snapfilter zfs.FilesystemVersionFilter) (h Handler) {
	return Handler{logger, dsfilter, snapfilter}
}

func registerEndpoints(server rpc.RPCServer, handler Handler) (err error) {
	err = server.RegisterEndpoint("FilesystemRequest", handler.HandleFilesystemRequest)
	if err != nil {
		panic(err)
	}
	err = server.RegisterEndpoint("FilesystemVersionsRequest", handler.HandleFilesystemVersionsRequest)
	if err != nil {
		panic(err)
	}
	err = server.RegisterEndpoint("InitialTransferRequest", handler.HandleInitialTransferRequest)
	if err != nil {
		panic(err)
	}
	err = server.RegisterEndpoint("IncrementalTransferRequest", handler.HandleIncrementalTransferRequest)
	if err != nil {
		panic(err)
	}
	return nil
}

func (h Handler) HandleFilesystemRequest(r *FilesystemRequest, roots *[]*zfs.DatasetPath) (err error) {

	log := h.logger.WithField("endpoint", "FilesystemRequest")

	log.WithField("request", r).Debug("request")
	log.WithField("dataset_filter", h.dsf).Debug("dsf")

	allowed, err := zfs.ZFSListMapping(h.dsf)
	if err != nil {
		log.WithError(err).Error("error listing filesystems")
		return
	}

	log.WithField("response", allowed).Debug("response")
	*roots = allowed
	return
}

func (h Handler) HandleFilesystemVersionsRequest(r *FilesystemVersionsRequest, versions *[]zfs.FilesystemVersion) (err error) {

	log := h.logger.WithField("endpoint", "FilesystemVersionsRequest")

	log.WithField("request", r).Debug("request")

	// allowed to request that?
	if h.pullACLCheck(r.Filesystem, nil); err != nil {
		log.WithError(err).Warn("pull ACL check failed")
		return
	}

	// find our versions
	vs, err := zfs.ZFSListFilesystemVersions(r.Filesystem, h.fsvf)
	if err != nil {
		log.WithError(err).Error("cannot list filesystem versions")
		return
	}

	log.WithField("resposne", vs).Debug("response")

	*versions = vs
	return

}

func (h Handler) HandleInitialTransferRequest(r *InitialTransferRequest, stream *io.Reader) (err error) {

	log := h.logger.WithField("endpoint", "InitialTransferRequest")

	log.WithField("request", r).Debug("request")
	if err = h.pullACLCheck(r.Filesystem, &r.FilesystemVersion); err != nil {
		log.WithError(err).Warn("pull ACL check failed")
		return
	}

	log.Debug("invoking zfs send")

	s, err := zfs.ZFSSend(r.Filesystem, &r.FilesystemVersion, nil)
	if err != nil {
		log.WithError(err).Error("cannot send filesystem")
	}
	*stream = s

	return

}

func (h Handler) HandleIncrementalTransferRequest(r *IncrementalTransferRequest, stream *io.Reader) (err error) {

	log := h.logger.WithField("endpoint", "IncrementalTransferRequest")
	log.WithField("request", r).Debug("request")
	if err = h.pullACLCheck(r.Filesystem, &r.From); err != nil {
		log.WithError(err).Warn("pull ACL check failed")
		return
	}
	if err = h.pullACLCheck(r.Filesystem, &r.To); err != nil {
		log.WithError(err).Warn("pull ACL check failed")
		return
	}

	log.Debug("invoking zfs send")

	s, err := zfs.ZFSSend(r.Filesystem, &r.From, &r.To)
	if err != nil {
		log.WithError(err).Error("cannot send filesystem")
	}

	*stream = s
	return

}

func (h Handler) pullACLCheck(p *zfs.DatasetPath, v *zfs.FilesystemVersion) (err error) {
	var fsAllowed, vAllowed bool
	fsAllowed, err = h.dsf.Filter(p)
	if err != nil {
		err = fmt.Errorf("error evaluating ACL: %s", err)
		return
	}
	if !fsAllowed {
		err = fmt.Errorf("ACL prohibits access to %s", p.ToString())
		return
	}
	if v == nil {
		return
	}

	vAllowed, err = h.fsvf.Filter(*v)
	if err != nil {
		err = errors.Wrap(err, "error evaluating version filter")
		return
	}
	if !vAllowed {
		err = fmt.Errorf("ACL prohibits access to %s", v.ToAbsPath(p))
		return
	}
	return
}
