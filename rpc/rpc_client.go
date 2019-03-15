package rpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"

	"github.com/google/uuid"
	"github.com/zrepl/zrepl/replication/logic"
	"github.com/zrepl/zrepl/replication/logic/pdu"
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

var _ logic.Endpoint = &Client{}
var _ logic.Sender = &Client{}
var _ logic.Receiver = &Client{}

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

func (c *Client) WaitForConnectivity(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	msg := uuid.New().String()
	req := pdu.PingReq{Message: msg}
	var ctrlOk, dataOk int32
	loggers := GetLoggersOrPanic(ctx)
	var wg sync.WaitGroup
	wg.Add(2)
	checkRes := func(res *pdu.PingRes, err error, logger Logger, okVar *int32) {
		if err == nil && res.GetEcho() != req.GetMessage() {
			err = errors.New("pilot message not echoed correctly")
		}
		if err == context.Canceled {
			err = nil
		}
		if err != nil {
			logger.WithError(err).Error("ping failed")
			atomic.StoreInt32(okVar, 0)
			cancel()
		} else {
			atomic.StoreInt32(okVar, 1)
		}
	}
	go func() {
		defer wg.Done()
		ctrl, ctrlErr := c.controlClient.Ping(ctx, &req, grpc.FailFast(false))
		checkRes(ctrl, ctrlErr, loggers.Control, &ctrlOk)
	}()
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			data, dataErr := c.dataClient.ReqPing(ctx, &req)
			// dataClient uses transport.Connecter, which doesn't expose FailFast(false)
			// => we need to mask dial timeouts
			if err, ok := dataErr.(interface{ Temporary() bool }); ok && err.Temporary() {
				// Rate-limit pings here in case Temporary() is a mis-classification
				// or returns immediately (this is a tight loop in that case)
				// TODO keep this in lockstep with controlClient
				// 		=> don't use FailFast for control, but check that both control and data worked
				time.Sleep(envconst.Duration("ZREPL_RPC_DATACONN_PING_SLEEP", 1*time.Second))
				continue
			}
			// it's not a dial timeout, 
			checkRes(data, dataErr, loggers.Data, &dataOk)
			return
		}
	}()
	wg.Wait()
	var what string
	if ctrlOk == 1 && dataOk == 1 {
		return nil
	}
	if ctrlOk == 0 {
		what += "control"
	}
	if dataOk == 0 {
		if len(what) > 0 {
			what += " and data"
		} else {
			what += "data"
		}
	}
	return fmt.Errorf("%s rpc failed to respond to ping rpcs", what)
}

func (c *Client) ResetConnectBackoff() {
	c.controlConn.ResetConnectBackoff()
}
