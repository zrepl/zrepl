package rpc

import (
	"context"
	"net"
	"time"

	"google.golang.org/grpc"

	"github.com/zrepl/zrepl/replication"
	"github.com/zrepl/zrepl/replication/pdu"
	"github.com/zrepl/zrepl/rpc/dataconn"
	"github.com/zrepl/zrepl/rpc/grpcclientidentity/grpchelper"
	"github.com/zrepl/zrepl/rpc/versionhandshake"
	"github.com/zrepl/zrepl/transport"
	"github.com/zrepl/zrepl/util/envconst"
	"github.com/zrepl/zrepl/zfs"
)

// Client implements the active side of a replication setup.
// It satisfies the Endpoint, Sender and Receiver interface defined by package replication.
type Client struct {
	dataClient    *dataconn.Client
	controlClient pdu.ReplicationClient // this the grpc client instance, see constructor
	controlConn   *grpc.ClientConn
	loggers       Loggers
}

var _ replication.Endpoint = &Client{}
var _ replication.Sender = &Client{}
var _ replication.Receiver = &Client{}

type DialContextFunc = func(ctx context.Context, network string, addr string) (net.Conn, error)

// config must be validated, NewClient will panic if it is not valid
func NewClient(cn transport.Connecter, loggers Loggers) *Client {

	cn = versionhandshake.Connecter(cn, envconst.Duration("ZREPL_RPC_CLIENT_VERSIONHANDSHAKE_TIMEOUT", 10*time.Second))

	muxedConnecter := mux(cn)

	c := &Client{
		loggers: loggers,
	}
	grpcConn := grpchelper.ClientConn(muxedConnecter.control, loggers.Control)

	go func() {
		for {
			state := grpcConn.GetState()
			loggers.General.WithField("grpc_state", state.String()).Debug("grpc state change")
			grpcConn.WaitForStateChange(context.TODO(), state)
		}
	}()
	c.controlClient = pdu.NewReplicationClient(grpcConn)
	c.controlConn = grpcConn

	c.dataClient = dataconn.NewClient(muxedConnecter.data, loggers.Data)
	return c
}

func (c *Client) Close() {
	if err := c.controlConn.Close(); err != nil {
		c.loggers.General.WithError(err).Error("cannot cloe control connection")
	}
	// TODO c.dataClient should have Close()
}

// callers must ensure that the returned io.ReadCloser is closed
// TODO expose dataClient interface to the outside world
func (c *Client) Send(ctx context.Context, r *pdu.SendReq) (*pdu.SendRes, zfs.StreamCopier, error) {
	// TODO the returned sendStream may return a read error created by the remote side
	res, streamCopier, err := c.dataClient.ReqSend(ctx, r)
	if err != nil {
		return nil, nil, nil
	}
	if streamCopier == nil {
		return res, nil, nil
	}

	return res, streamCopier, nil

}

func (c *Client) Receive(ctx context.Context, req *pdu.ReceiveReq, streamCopier zfs.StreamCopier) (*pdu.ReceiveRes, error) {
	return c.dataClient.ReqRecv(ctx, req, streamCopier)
}

func (c *Client) ListFilesystems(ctx context.Context, in *pdu.ListFilesystemReq) (*pdu.ListFilesystemRes, error) {
	return c.controlClient.ListFilesystems(ctx, in)
}

func (c *Client) ListFilesystemVersions(ctx context.Context, in *pdu.ListFilesystemVersionsReq) (*pdu.ListFilesystemVersionsRes, error) {
	return c.controlClient.ListFilesystemVersions(ctx, in)
}

func (c *Client) DestroySnapshots(ctx context.Context, in *pdu.DestroySnapshotsReq) (*pdu.DestroySnapshotsRes, error) {
	return c.controlClient.DestroySnapshots(ctx, in)
}

func (c *Client) ReplicationCursor(ctx context.Context, in *pdu.ReplicationCursorReq) (*pdu.ReplicationCursorRes, error) {
	return c.controlClient.ReplicationCursor(ctx, in)
}

func (c *Client) ResetConnectBackoff() {
	c.controlConn.ResetConnectBackoff()
}
