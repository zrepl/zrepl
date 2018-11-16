// Package endpoint implements replication endpoints for use with package replication.
package endpoint

import (
	"bytes"
	"context"
	"fmt"
	"github.com/golang/protobuf/proto"
	"github.com/pkg/errors"
	"github.com/problame/go-streamrpc"
	"github.com/zrepl/zrepl/replication"
	"github.com/zrepl/zrepl/replication/pdu"
	"github.com/zrepl/zrepl/zfs"
	"io"
)

// Sender implements replication.ReplicationEndpoint for a sending side
type Sender struct {
	FSFilter                zfs.DatasetFilter
}

func NewSender(fsf zfs.DatasetFilter) *Sender {
	return &Sender{FSFilter: fsf}
}

func (s *Sender) filterCheckFS(fs string) (*zfs.DatasetPath, error) {
	dp, err := zfs.NewDatasetPath(fs)
	if err != nil {
		return nil, err
	}
	if dp.Length() == 0 {
		return nil, errors.New("empty filesystem not allowed")
	}
	pass, err := s.FSFilter.Filter(dp)
	if err != nil {
		return nil, err
	}
	if !pass {
		return nil, replication.NewFilteredError(fs)
	}
	return dp, nil
}

func (p *Sender) ListFilesystems(ctx context.Context) ([]*pdu.Filesystem, error) {
	fss, err := zfs.ZFSListMapping(p.FSFilter)
	if err != nil {
		return nil, err
	}
	rfss := make([]*pdu.Filesystem, len(fss))
	for i := range fss {
		rfss[i] = &pdu.Filesystem{
			Path: fss[i].ToString(),
			// FIXME: not supporting ResumeToken yet
		}
	}
	return rfss, nil
}

func (p *Sender) ListFilesystemVersions(ctx context.Context, fs string) ([]*pdu.FilesystemVersion, error) {
	lp, err := p.filterCheckFS(fs)
	if err != nil {
		return nil, err
	}
	fsvs, err := zfs.ZFSListFilesystemVersions(lp, nil)
	if err != nil {
		return nil, err
	}
	rfsvs := make([]*pdu.FilesystemVersion, len(fsvs))
	for i := range fsvs {
		rfsvs[i] = pdu.FilesystemVersionFromZFS(&fsvs[i])
	}
	return rfsvs, nil
}

func (p *Sender) Send(ctx context.Context, r *pdu.SendReq) (*pdu.SendRes, io.ReadCloser, error) {
	_, err := p.filterCheckFS(r.Filesystem)
	if err != nil {
		return nil, nil, err
	}

	if r.DryRun {
		si, err := zfs.ZFSSendDry(r.Filesystem, r.From, r.To, "")
		if err != nil {
			return nil, nil, err
		}
		var expSize int64 = 0 // protocol says 0 means no estimate
		if si.SizeEstimate != -1 { // but si returns -1 for no size estimate
			expSize = si.SizeEstimate
		}
		return &pdu.SendRes{ExpectedSize: expSize}, nil, nil
	} else {
		stream, err := zfs.ZFSSend(ctx, r.Filesystem, r.From, r.To, "")
		if err != nil {
			return nil, nil, err
		}
		return &pdu.SendRes{}, stream, nil
	}
}

func (p *Sender) DestroySnapshots(ctx context.Context, req *pdu.DestroySnapshotsReq) (*pdu.DestroySnapshotsRes, error) {
	dp, err := p.filterCheckFS(req.Filesystem)
	if err != nil {
		return nil, err
	}
	return doDestroySnapshots(ctx, dp, req.Snapshots)
}

func (p *Sender) ReplicationCursor(ctx context.Context, req *pdu.ReplicationCursorReq) (*pdu.ReplicationCursorRes, error) {
	dp, err := p.filterCheckFS(req.Filesystem)
	if err != nil {
		return nil, err
	}

	switch op := req.Op.(type) {
	case *pdu.ReplicationCursorReq_Get:
		cursor, err := zfs.ZFSGetReplicationCursor(dp)
		if err != nil {
			return nil, err
		}
		if cursor == nil {
			return &pdu.ReplicationCursorRes{Result: &pdu.ReplicationCursorRes_Notexist{Notexist: true}}, nil
		}
		return &pdu.ReplicationCursorRes{Result: &pdu.ReplicationCursorRes_Guid{Guid: cursor.Guid}}, nil
	case *pdu.ReplicationCursorReq_Set:
		guid, err := zfs.ZFSSetReplicationCursor(dp, op.Set.Snapshot)
		if err != nil {
			return nil, err
		}
		return &pdu.ReplicationCursorRes{Result: &pdu.ReplicationCursorRes_Guid{Guid: guid}}, nil
	default:
		return nil, errors.Errorf("unknown op %T", op)
	}
}

type FSFilter interface { // FIXME unused
	Filter(path *zfs.DatasetPath) (pass bool, err error)
}

// FIXME: can we get away without error types here?
type FSMap interface { // FIXME unused
	FSFilter
	Map(path *zfs.DatasetPath) (*zfs.DatasetPath, error)
	Invert() (FSMap, error)
	AsFilter() FSFilter
}

// Receiver implements replication.ReplicationEndpoint for a receiving side
type Receiver struct {
	root *zfs.DatasetPath
}

func NewReceiver(rootDataset *zfs.DatasetPath) (*Receiver, error) {
	if rootDataset.Length() <= 0 {
		return nil, errors.New("root dataset must not be an empty path")
	}
	return &Receiver{root: rootDataset.Copy()}, nil
}

type subroot struct {
	localRoot *zfs.DatasetPath
}

var _ zfs.DatasetFilter = subroot{}

// Filters local p
func (f subroot) Filter(p *zfs.DatasetPath) (pass bool, err error) {
	return p.HasPrefix(f.localRoot) && !p.Equal(f.localRoot), nil
}

func (f subroot) MapToLocal(fs string) (*zfs.DatasetPath, error) {
	p, err := zfs.NewDatasetPath(fs)
	if err != nil {
		return nil, err
	}
	if p.Length() == 0 {
		return nil, errors.Errorf("cannot map empty filesystem")
	}
	c := f.localRoot.Copy()
	c.Extend(p)
	return c, nil
}

func (e *Receiver) ListFilesystems(ctx context.Context) ([]*pdu.Filesystem, error) {
	filtered, err := zfs.ZFSListMapping(subroot{e.root})
	if err != nil {
		return nil, err
	}
	// present without prefix, and only those that are not placeholders
	fss := make([]*pdu.Filesystem, 0, len(filtered))
	for _, a := range filtered {
		ph, err := zfs.ZFSIsPlaceholderFilesystem(a)
		if err != nil {
			getLogger(ctx).
				WithError(err).
				WithField("fs", a).
				Error("inconsistent placeholder property")
			return nil, errors.New("server error, see logs") // don't leak path
		}
		if ph {
			continue
		}
		a.TrimPrefix(e.root)
		fss = append(fss, &pdu.Filesystem{Path: a.ToString()})
	}
	return fss, nil
}

func (e *Receiver) ListFilesystemVersions(ctx context.Context, fs string) ([]*pdu.FilesystemVersion, error) {
	lp, err := subroot{e.root}.MapToLocal(fs)
	if err != nil {
		return nil, err
	}

	fsvs, err := zfs.ZFSListFilesystemVersions(lp, nil)
	if err != nil {
		return nil, err
	}

	rfsvs := make([]*pdu.FilesystemVersion, len(fsvs))
	for i := range fsvs {
		rfsvs[i] = pdu.FilesystemVersionFromZFS(&fsvs[i])
	}

	return rfsvs, nil
}

func (e *Receiver) Receive(ctx context.Context, req *pdu.ReceiveReq, sendStream io.ReadCloser) error {
	defer sendStream.Close()

	lp, err := subroot{e.root}.MapToLocal(req.Filesystem)
	if err != nil {
		return err
	}

	getLogger(ctx).Debug("incoming Receive")

	// create placeholder parent filesystems as appropriate
	var visitErr error
	f := zfs.NewDatasetPathForest()
	f.Add(lp)
	getLogger(ctx).Debug("begin tree-walk")
	f.WalkTopDown(func(v zfs.DatasetPathVisit) (visitChildTree bool) {
		if v.Path.Equal(lp) {
			return false
		}
		_, err := zfs.ZFSGet(v.Path, []string{zfs.ZREPL_PLACEHOLDER_PROPERTY_NAME})
		if err != nil {
			// interpret this as an early exit of the zfs binary due to the fs not existing
			if err := zfs.ZFSCreatePlaceholderFilesystem(v.Path); err != nil {
				getLogger(ctx).
					WithError(err).
					WithField("placeholder_fs", v.Path).
					Error("cannot create placeholder filesystem")
				visitErr = err
				return false
			}
		}
		getLogger(ctx).WithField("filesystem", v.Path.ToString()).Debug("exists")
		return true // leave this fs as is
	})
	getLogger(ctx).WithField("visitErr", visitErr).Debug("complete tree-walk")

	if visitErr != nil {
		return visitErr
	}

	needForceRecv := false
	props, err := zfs.ZFSGet(lp, []string{zfs.ZREPL_PLACEHOLDER_PROPERTY_NAME})
	if err == nil {
		if isPlaceholder, _ := zfs.IsPlaceholder(lp, props.Get(zfs.ZREPL_PLACEHOLDER_PROPERTY_NAME)); isPlaceholder {
			needForceRecv = true
		}
	}

	args := make([]string, 0, 1)
	if needForceRecv {
		args = append(args, "-F")
	}

	getLogger(ctx).Debug("start receive command")

	if err := zfs.ZFSRecv(ctx, lp.ToString(), sendStream, args...); err != nil {
		getLogger(ctx).
			WithError(err).
			WithField("args", args).
			Error("zfs receive failed")
		sendStream.Close()
		return err
	}
	return nil
}

func (e *Receiver) DestroySnapshots(ctx context.Context, req *pdu.DestroySnapshotsReq) (*pdu.DestroySnapshotsRes, error) {
	lp, err := subroot{e.root}.MapToLocal(req.Filesystem)
	if err != nil {
		return nil, err
	}
	return doDestroySnapshots(ctx, lp, req.Snapshots)
}

func doDestroySnapshots(ctx context.Context, lp *zfs.DatasetPath, snaps []*pdu.FilesystemVersion) (*pdu.DestroySnapshotsRes, error) {
	fsvs := make([]*zfs.FilesystemVersion, len(snaps))
	for i, fsv := range snaps {
		if fsv.Type != pdu.FilesystemVersion_Snapshot {
			return nil, fmt.Errorf("version %q is not a snapshot", fsv.Name)
		}
		var err error
		fsvs[i], err = fsv.ZFSFilesystemVersion()
		if err != nil {
			return nil, err
		}
	}
	res := &pdu.DestroySnapshotsRes{
		Results: make([]*pdu.DestroySnapshotRes, len(fsvs)),
	}
	for i, fsv := range fsvs {
		err := zfs.ZFSDestroyFilesystemVersion(lp, fsv)
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		}
		res.Results[i] = &pdu.DestroySnapshotRes{
			Snapshot: pdu.FilesystemVersionFromZFS(fsv),
			Error:    errMsg,
		}
	}
	return res, nil
}

// =-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=
// RPC STUBS
// =-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=

const (
	RPCListFilesystems        = "ListFilesystems"
	RPCListFilesystemVersions = "ListFilesystemVersions"
	RPCReceive                = "Receive"
	RPCSend                   = "Send"
	RPCSDestroySnapshots      = "DestroySnapshots"
	RPCReplicationCursor      = "ReplicationCursor"
)

// Remote implements an endpoint stub that uses streamrpc as a transport.
type Remote struct {
	c *streamrpc.Client
}

func NewRemote(c *streamrpc.Client) Remote {
	return Remote{c}
}

func (s Remote) ListFilesystems(ctx context.Context) ([]*pdu.Filesystem, error) {
	req := pdu.ListFilesystemReq{}
	b, err := proto.Marshal(&req)
	if err != nil {
		return nil, err
	}
	rb, rs, err := s.c.RequestReply(ctx, RPCListFilesystems, bytes.NewBuffer(b), nil)
	if err != nil {
		return nil, err
	}
	if rs != nil {
		rs.Close()
		return nil, errors.New("response contains unexpected stream")
	}
	var res pdu.ListFilesystemRes
	if err := proto.Unmarshal(rb.Bytes(), &res); err != nil {
		return nil, err
	}
	return res.Filesystems, nil
}

func (s Remote) ListFilesystemVersions(ctx context.Context, fs string) ([]*pdu.FilesystemVersion, error) {
	req := pdu.ListFilesystemVersionsReq{
		Filesystem: fs,
	}
	b, err := proto.Marshal(&req)
	if err != nil {
		return nil, err
	}
	rb, rs, err := s.c.RequestReply(ctx, RPCListFilesystemVersions, bytes.NewBuffer(b), nil)
	if err != nil {
		return nil, err
	}
	if rs != nil {
		rs.Close()
		return nil, errors.New("response contains unexpected stream")
	}
	var res pdu.ListFilesystemVersionsRes
	if err := proto.Unmarshal(rb.Bytes(), &res); err != nil {
		return nil, err
	}
	return res.Versions, nil
}

func (s Remote) Send(ctx context.Context, r *pdu.SendReq) (*pdu.SendRes, io.ReadCloser, error) {
	b, err := proto.Marshal(r)
	if err != nil {
		return nil, nil, err
	}
	rb, rs, err := s.c.RequestReply(ctx, RPCSend, bytes.NewBuffer(b), nil)
	if err != nil {
		return nil, nil, err
	}
	if !r.DryRun && rs == nil {
		return nil, nil, errors.New("response does not contain a stream")
	}
	if r.DryRun && rs != nil {
		rs.Close()
		return nil, nil, errors.New("response contains unexpected stream (was dry run)")
	}
	var res pdu.SendRes
	if err := proto.Unmarshal(rb.Bytes(), &res); err != nil {
		rs.Close()
		return nil, nil, err
	}
	return &res, rs, nil
}

func (s Remote) Receive(ctx context.Context, r *pdu.ReceiveReq, sendStream io.ReadCloser) error {
	defer sendStream.Close()
	b, err := proto.Marshal(r)
	if err != nil {
		return err
	}
	rb, rs, err := s.c.RequestReply(ctx, RPCReceive, bytes.NewBuffer(b), sendStream)
	getLogger(ctx).WithField("err", err).Debug("Remote.Receive RequestReplyReturned")
	if err != nil {
		return err
	}
	if rs != nil {
		rs.Close()
		return errors.New("response contains unexpected stream")
	}
	var res pdu.ReceiveRes
	if err := proto.Unmarshal(rb.Bytes(), &res); err != nil {
		return err
	}
	return nil
}

func (s Remote) DestroySnapshots(ctx context.Context, r *pdu.DestroySnapshotsReq) (*pdu.DestroySnapshotsRes, error) {
	b, err := proto.Marshal(r)
	if err != nil {
		return nil, err
	}
	rb, rs, err := s.c.RequestReply(ctx, RPCSDestroySnapshots, bytes.NewBuffer(b), nil)
	if err != nil {
		return nil, err
	}
	if rs != nil {
		rs.Close()
		return nil, errors.New("response contains unexpected stream")
	}
	var res pdu.DestroySnapshotsRes
	if err := proto.Unmarshal(rb.Bytes(), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func (s Remote) ReplicationCursor(ctx context.Context, req *pdu.ReplicationCursorReq) (*pdu.ReplicationCursorRes, error) {
	b, err := proto.Marshal(req)
	if err != nil {
		return nil, err
	}
	rb, rs, err := s.c.RequestReply(ctx, RPCReplicationCursor, bytes.NewBuffer(b), nil)
	if err != nil {
		return nil, err
	}
	if rs != nil {
		rs.Close()
		return nil, errors.New("response contains unexpected stream")
	}
	var res pdu.ReplicationCursorRes
	if err := proto.Unmarshal(rb.Bytes(), &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Handler implements the server-side streamrpc.HandlerFunc for a Remote endpoint stub.
type Handler struct {
	ep replication.Endpoint
}

func NewHandler(ep replication.Endpoint) Handler {
	return Handler{ep}
}

func (a *Handler) Handle(ctx context.Context, endpoint string, reqStructured *bytes.Buffer, reqStream io.ReadCloser) (resStructured *bytes.Buffer, resStream io.ReadCloser, err error) {

	switch endpoint {
	case RPCListFilesystems:
		var req pdu.ListFilesystemReq
		if err := proto.Unmarshal(reqStructured.Bytes(), &req); err != nil {
			return nil, nil, err
		}
		fsses, err := a.ep.ListFilesystems(ctx)
		if err != nil {
			return nil, nil, err
		}
		res := &pdu.ListFilesystemRes{
			Filesystems: fsses,
		}
		b, err := proto.Marshal(res)
		if err != nil {
			return nil, nil, err
		}
		return bytes.NewBuffer(b), nil, nil

	case RPCListFilesystemVersions:

		var req pdu.ListFilesystemVersionsReq
		if err := proto.Unmarshal(reqStructured.Bytes(), &req); err != nil {
			return nil, nil, err
		}
		fsvs, err := a.ep.ListFilesystemVersions(ctx, req.Filesystem)
		if err != nil {
			return nil, nil, err
		}
		res := &pdu.ListFilesystemVersionsRes{
			Versions: fsvs,
		}
		b, err := proto.Marshal(res)
		if err != nil {
			return nil, nil, err
		}
		return bytes.NewBuffer(b), nil, nil

	case RPCSend:

		sender, ok := a.ep.(replication.Sender)
		if !ok {
			goto Err
		}

		var req pdu.SendReq
		if err := proto.Unmarshal(reqStructured.Bytes(), &req); err != nil {
			return nil, nil, err
		}
		res, sendStream, err := sender.Send(ctx, &req)
		if err != nil {
			return nil, nil, err
		}
		b, err := proto.Marshal(res)
		if err != nil {
			return nil, nil, err
		}
		return bytes.NewBuffer(b), sendStream, err

	case RPCReceive:

		receiver, ok := a.ep.(replication.Receiver)
		if !ok {
			goto Err
		}

		var req pdu.ReceiveReq
		if err := proto.Unmarshal(reqStructured.Bytes(), &req); err != nil {
			return nil, nil, err
		}
		err := receiver.Receive(ctx, &req, reqStream)
		if err != nil {
			return nil, nil, err
		}
		b, err := proto.Marshal(&pdu.ReceiveRes{})
		if err != nil {
			return nil, nil, err
		}
		return bytes.NewBuffer(b), nil, err

	case RPCSDestroySnapshots:

		var req pdu.DestroySnapshotsReq
		if err := proto.Unmarshal(reqStructured.Bytes(), &req); err != nil {
			return nil, nil, err
		}

		res, err := a.ep.DestroySnapshots(ctx, &req)
		if err != nil {
			return nil, nil, err
		}
		b, err := proto.Marshal(res)
		if err != nil {
			return nil, nil, err
		}
		return bytes.NewBuffer(b), nil, nil

	case RPCReplicationCursor:

		sender, ok := a.ep.(replication.Sender)
		if !ok {
			goto Err
		}

		var req pdu.ReplicationCursorReq
		if err := proto.Unmarshal(reqStructured.Bytes(), &req); err != nil {
			return nil, nil, err
		}
		res, err := sender.ReplicationCursor(ctx, &req)
		if err != nil {
			return nil, nil, err
		}
		b, err := proto.Marshal(res)
		if err != nil {
			return nil, nil, err
		}
		return bytes.NewBuffer(b), nil, nil

	}
Err:
	return nil, nil, errors.New("no handler for given endpoint")
}
