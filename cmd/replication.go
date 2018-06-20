package cmd

import (
	"fmt"
	"github.com/zrepl/zrepl/cmd/replication"
	"github.com/problame/go-streamrpc"
	"github.com/zrepl/zrepl/zfs"
	"io"
	"github.com/pkg/errors"
	"github.com/golang/protobuf/proto"
	"bytes"
	"os"
)

type InitialReplPolicy string

const (
	InitialReplPolicyMostRecent InitialReplPolicy = "most_recent"
	InitialReplPolicyAll        InitialReplPolicy = "all"
)

const DEFAULT_INITIAL_REPL_POLICY = InitialReplPolicyMostRecent

// SenderEndpoint implements replication.ReplicationEndpoint for a sending side
type SenderEndpoint struct {
	FSFilter 				zfs.DatasetFilter
	FilesystemVersionFilter zfs.FilesystemVersionFilter
}

func NewSenderEndpoint(fsf zfs.DatasetFilter, fsvf zfs.FilesystemVersionFilter) *SenderEndpoint {
	return &SenderEndpoint{fsf, fsvf}
}

func (p *SenderEndpoint) ListFilesystems() ([]*replication.Filesystem, error) {
	fss, err := zfs.ZFSListMapping(p.FSFilter)
	if err != nil {
		return nil, err
	}
	rfss := make([]*replication.Filesystem, len(fss))
	for i := range fss {
		rfss[i] = &replication.Filesystem{
			Path: fss[i].ToString(),
			// FIXME: not supporting ResumeToken yet
		}
	}
	return rfss, nil
}

func (p *SenderEndpoint) ListFilesystemVersions(fs string) ([]*replication.FilesystemVersion, error) {
	dp, err := zfs.NewDatasetPath(fs)
	if err != nil {
		return nil, err
	}
	pass, err := p.FSFilter.Filter(dp)
	if err != nil {
		return nil, err
	}
	if !pass {
		return nil, replication.NewFilteredError(fs)
	}
	fsvs, err := zfs.ZFSListFilesystemVersions(dp, p.FilesystemVersionFilter)
	if err != nil {
		return nil, err
	}
	rfsvs := make([]*replication.FilesystemVersion, len(fsvs))
	for i := range fsvs {
		rfsvs[i] = replication.FilesystemVersionFromZFS(fsvs[i])
	}
	return rfsvs, nil
}

func (p *SenderEndpoint) Send(r *replication.SendReq) (*replication.SendRes, io.Reader, error) {
	os.Stderr.WriteString("sending " + r.String() + "\n")
	dp, err := zfs.NewDatasetPath(r.Filesystem)
	if err != nil {
		return nil, nil, err
	}
	pass, err := p.FSFilter.Filter(dp)
	if err != nil {
		return nil, nil, err
	}
	if !pass {
		return nil, nil, replication.NewFilteredError(r.Filesystem)
	}
	stream, err := zfs.ZFSSend(r.Filesystem, r.From, r.To)
	if err != nil {
		return nil, nil, err
	}
	return &replication.SendRes{}, stream, nil
}

func (p *SenderEndpoint) Receive(r *replication.ReceiveReq, sendStream io.Reader) (error) {
	return fmt.Errorf("sender endpoint does not receive")
}


// ReceiverEndpoint implements replication.ReplicationEndpoint for a receiving side
type ReceiverEndpoint struct {
	fsmapInv *DatasetMapFilter
	fsmap *DatasetMapFilter
	fsvf zfs.FilesystemVersionFilter
}

func NewReceiverEndpoint(fsmap *DatasetMapFilter, fsvf zfs.FilesystemVersionFilter) (*ReceiverEndpoint, error) {
	fsmapInv, err := fsmap.Invert()
	if err != nil {
		return nil, err
	}
	return &ReceiverEndpoint{fsmapInv, fsmap, fsvf}, nil
}

func (e *ReceiverEndpoint) ListFilesystems() ([]*replication.Filesystem, error) {
	filtered, err := zfs.ZFSListMapping(e.fsmapInv.AsFilter())
	if err != nil {
		return nil, errors.Wrap(err, "error checking client permission")
	}
	fss := make([]*replication.Filesystem, len(filtered))
	for i, a := range filtered {
		mapped, err := e.fsmapInv.Map(a)
		if err != nil {
			return nil, err
		}
		fss[i] = &replication.Filesystem{Path: mapped.ToString()}
	}
	return fss, nil
}

func (e *ReceiverEndpoint) ListFilesystemVersions(fs string) ([]*replication.FilesystemVersion, error) {
	p, err := zfs.NewDatasetPath(fs)
	if err != nil {
		return nil, err
	}
	lp, err := e.fsmap.Map(p)
	if err != nil {
		return nil, err
	}
	if lp == nil {
		return nil, errors.New("access to filesystem denied")
	}

	fsvs, err := zfs.ZFSListFilesystemVersions(lp, e.fsvf)
	if err != nil {
		return nil, err
	}

	rfsvs := make([]*replication.FilesystemVersion, len(fsvs))
	for i := range fsvs {
		rfsvs[i] = replication.FilesystemVersionFromZFS(fsvs[i])
	}

	return rfsvs, nil
}

func (e *ReceiverEndpoint) Send(req *replication.SendReq) (*replication.SendRes, io.Reader, error) {
	return nil, nil, errors.New("receiver endpoint does not send")
}

func (e *ReceiverEndpoint) Receive(req *replication.ReceiveReq, sendStream io.Reader) error {
	p, err := zfs.NewDatasetPath(req.Filesystem)
	if err != nil {
		return err
	}
	lp, err := e.fsmap.Map(p)
	if err != nil {
		return err
	}
	if lp == nil {
		return errors.New("receive to filesystem denied")
	}

	// create placeholder parent filesystems as appropriate
	var visitErr error
	f := zfs.NewDatasetPathForest()
	f.Add(lp)
	f.WalkTopDown(func(v zfs.DatasetPathVisit) (visitChildTree bool) {
		if v.Path.Equal(lp) {
			return false
		}
		_, err := zfs.ZFSGet(v.Path, []string{zfs.ZREPL_PLACEHOLDER_PROPERTY_NAME})
		if err != nil {
			os.Stderr.WriteString("error zfsget " + err.Error() + "\n")
			// interpret this as an early exit of the zfs binary due to the fs not existing
			if err := zfs.ZFSCreatePlaceholderFilesystem(v.Path); err != nil {
				os.Stderr.WriteString("error creating placeholder " + v.Path.ToString() + "\n")
				visitErr = err
				return false
			}
		}
		os.Stderr.WriteString(v.Path.ToString() + " exists\n")
		return true // leave this fs as is
	})

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

	os.Stderr.WriteString("receiving...\n")

	if err := zfs.ZFSRecv(lp.ToString(), sendStream, args...); err != nil {
		// FIXME sendStream is on the wire and contains data, if we don't consume it, wire must be closed
		return err
	}
	return nil
}

// =-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=
// RPC STUBS
// =-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=-=


const (
	RPCListFilesystems = "ListFilesystems"
	RPCListFilesystemVersions = "ListFilesystemVersions"
	RPCReceive = "Receive"
	RPCSend = "Send"
)

type RemoteEndpoint struct {
	*streamrpc.Client
}

func (s RemoteEndpoint) ListFilesystems() ([]*replication.Filesystem, error) {
	req := replication.ListFilesystemReq{}
	b, err := proto.Marshal(&req)
	if err != nil {
		return nil, err
	}
	rb, rs, err := s.RequestReply(RPCListFilesystems, bytes.NewBuffer(b), nil)
	if err != nil {
		return nil, err
	}
	if rs != nil {
		os.Stderr.WriteString(fmt.Sprintf("%#v\n", rs))
		s.Close() // FIXME
		return nil, errors.New("response contains unexpected stream")
	}
	var res replication.ListFilesystemRes
	if err := proto.Unmarshal(rb.Bytes(), &res); err != nil {
		return nil, err
	}
	return res.Filesystems, nil
}

func (s RemoteEndpoint) ListFilesystemVersions(fs string) ([]*replication.FilesystemVersion, error) {
	req := replication.ListFilesystemVersionsReq{
		Filesystem: fs,
	}
	b, err := proto.Marshal(&req)
	if err != nil {
		return nil, err
	}
	rb, rs, err := s.RequestReply(RPCListFilesystemVersions, bytes.NewBuffer(b), nil)
	if err != nil {
		return nil, err
	}
	if rs != nil {
		s.Close() // FIXME
		return nil, errors.New("response contains unexpected stream")
	}
	var res replication.ListFilesystemVersionsRes
	if err := proto.Unmarshal(rb.Bytes(), &res); err != nil {
		return nil, err
	}
	return res.Versions, nil
}

func (s RemoteEndpoint) Send(r *replication.SendReq) (*replication.SendRes, io.Reader, error) {
	b, err := proto.Marshal(r)
	if err != nil {
		return nil, nil, err
	}
	rb, rs, err := s.RequestReply(RPCSend, bytes.NewBuffer(b), nil)
	if err != nil {
		return nil, nil, err
	}
	if rs == nil {
		return nil, nil, errors.New("response does not contain a stream")
	}
	var res replication.SendRes
	if err := proto.Unmarshal(rb.Bytes(), &res); err != nil {
		s.Close() // FIXME
		return  nil, nil, err
	}
	// FIXME make sure the consumer will read the reader until the end...
	return &res, rs, nil
}

func (s RemoteEndpoint)	Receive(r *replication.ReceiveReq, sendStream io.Reader) (error) {
	b, err := proto.Marshal(r)
	if err != nil {
		return err
	}
	rb, rs, err := s.RequestReply(RPCReceive, bytes.NewBuffer(b), sendStream)
	if err != nil {
		s.Close() // FIXME
		return err
	}
	if rs != nil {
		return errors.New("response contains unexpected stream")
	}
	var res replication.ReceiveRes
	if err := proto.Unmarshal(rb.Bytes(), &res); err != nil {
		return err
	}
	return nil
}

type HandlerAdaptor struct {
	ep replication.ReplicationEndpoint
}

func (a *HandlerAdaptor) Handle(endpoint string, reqStructured *bytes.Buffer, reqStream io.Reader) (resStructured *bytes.Buffer, resStream io.Reader, err error) {

	switch endpoint {
	case RPCListFilesystems:
		var req replication.ListFilesystemReq
		if err := proto.Unmarshal(reqStructured.Bytes(), &req); err != nil {
			return nil, nil, err
		}
		fsses, err := a.ep.ListFilesystems()
		if err != nil {
			return nil, nil, err
		}
		res := &replication.ListFilesystemRes{
			Filesystems: fsses,
		}
		b, err := proto.Marshal(res)
		if err != nil {
			return nil, nil, err
		}
		return bytes.NewBuffer(b), nil, nil

	case RPCListFilesystemVersions:

		var req replication.ListFilesystemVersionsReq
		if err := proto.Unmarshal(reqStructured.Bytes(), &req); err != nil {
			return nil, nil, err
		}
		fsvs, err := a.ep.ListFilesystemVersions(req.Filesystem)
		if err != nil {
			return nil, nil, err
		}
		res := &replication.ListFilesystemVersionsRes{
			Versions: fsvs,
		}
		b, err := proto.Marshal(res)
		if err != nil {
			return nil, nil, err
		}
		return bytes.NewBuffer(b), nil, nil

	case RPCSend:

		var req replication.SendReq
		if err := proto.Unmarshal(reqStructured.Bytes(), &req); err != nil {
			return nil, nil, err
		}
		res, sendStream, err := a.ep.Send(&req)
		if err != nil {
			return nil, nil, err
		}
		b, err := proto.Marshal(res)
		if err != nil {
			return nil, nil, err
		}
		return bytes.NewBuffer(b), sendStream, err

	case RPCReceive:

		var req replication.ReceiveReq
		if err := proto.Unmarshal(reqStructured.Bytes(), &req); err != nil {
			return nil, nil, err
		}
		err := a.ep.Receive(&req, reqStream)
		if err != nil {
			return nil, nil, err
		}
		b, err := proto.Marshal(&replication.ReceiveRes{})
		if err != nil {
			return nil, nil, err
		}
		return bytes.NewBuffer(b), nil, err


	default:
		return nil, nil, errors.New("no handler for given endpoint")
	}
}
