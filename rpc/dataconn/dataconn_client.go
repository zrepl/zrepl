package dataconn

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"google.golang.org/protobuf/proto"

	"github.com/zrepl/zrepl/replication/logic/pdu"
	"github.com/zrepl/zrepl/rpc/dataconn/stream"
	"github.com/zrepl/zrepl/transport"
)

type Client struct {
	log Logger
	cn  transport.Connecter
}

func NewClient(connecter transport.Connecter, log Logger) *Client {
	return &Client{
		log: log,
		cn:  connecter,
	}
}

func (c *Client) send(ctx context.Context, conn *stream.Conn, endpoint string, req proto.Message, stream io.ReadCloser) error {

	var buf bytes.Buffer
	_, memErr := buf.WriteString(endpoint)
	if memErr != nil {
		panic(memErr)
	}
	if err := conn.WriteStreamedMessage(ctx, &buf, ReqHeader); err != nil {
		return err
	}

	protobufBytes, err := proto.Marshal(req)
	if err != nil {
		return err
	}
	protobuf := bytes.NewBuffer(protobufBytes)
	if err := conn.WriteStreamedMessage(ctx, protobuf, ReqStructured); err != nil {
		return err
	}

	if stream != nil {
		return conn.SendStream(ctx, stream, ZFSStream)
	} else {
		return nil
	}
}

type RemoteHandlerError struct {
	msg string
}

func (e *RemoteHandlerError) Error() string {
	return fmt.Sprintf("server error: %s", e.msg)
}

type ProtocolError struct {
	cause error
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("protocol error: %s", e.cause)
}

func (c *Client) recv(ctx context.Context, conn *stream.Conn, res proto.Message) error {

	headerBuf, err := conn.ReadStreamedMessage(ctx, ResponseHeaderMaxSize, ResHeader)
	if err != nil {
		return err
	}
	header := string(headerBuf)
	if strings.HasPrefix(header, responseHeaderHandlerErrorPrefix) {
		// FIXME distinguishable error type
		return &RemoteHandlerError{strings.TrimPrefix(header, responseHeaderHandlerErrorPrefix)}
	}
	if !strings.HasPrefix(header, responseHeaderHandlerOk) {
		return &ProtocolError{fmt.Errorf("invalid header: %q", header)}
	}

	protobuf, err := conn.ReadStreamedMessage(ctx, ResponseStructuredMaxSize, ResStructured)
	if err != nil {
		return err
	}
	if err := proto.Unmarshal(protobuf, res); err != nil {
		return &ProtocolError{fmt.Errorf("cannot unmarshal structured part of response: %s", err)}
	}
	return nil
}

func (c *Client) getWire(ctx context.Context) (*stream.Conn, error) {
	nc, err := c.cn.Connect(ctx)
	if err != nil {
		return nil, err
	}
	conn := stream.Wrap(nc, HeartbeatInterval, HeartbeatPeerTimeout)
	return conn, nil
}

func (c *Client) putWire(conn *stream.Conn) {
	if err := conn.Close(); err != nil {
		c.log.WithError(err).Error("error closing connection")
	}
}

func (c *Client) ReqSend(ctx context.Context, req *pdu.SendReq) (*pdu.SendRes, io.ReadCloser, error) {
	conn, err := c.getWire(ctx)
	if err != nil {
		return nil, nil, err
	}
	putWireOnReturn := true
	defer func() {
		if putWireOnReturn {
			c.putWire(conn)
		}
	}()

	if err := c.send(ctx, conn, EndpointSend, req, nil); err != nil {
		return nil, nil, err
	}

	var res pdu.SendRes
	if err := c.recv(ctx, conn, &res); err != nil {
		return nil, nil, err
	}

	var stream io.ReadCloser
	if !req.DryRun {
		putWireOnReturn = false
		stream, err = conn.ReadStream(ZFSStream, true) // no shadow
		if err != nil {
			return nil, nil, err
		}
	}

	return &res, stream, nil
}

func (c *Client) ReqRecv(ctx context.Context, req *pdu.ReceiveReq, stream io.ReadCloser) (*pdu.ReceiveRes, error) {
	defer c.log.Debug("ReqRecv returns")
	conn, err := c.getWire(ctx)
	if err != nil {
		return nil, err
	}

	// send and recv response concurrently to catch early exists of remote handler
	// (e.g. disk full, permission error, etc)

	type recvRes struct {
		res *pdu.ReceiveRes
		err error
	}
	recvErrChan := make(chan recvRes)
	go func() {
		res := &pdu.ReceiveRes{}
		if err := c.recv(ctx, conn, res); err != nil {
			recvErrChan <- recvRes{res, err}
		} else {
			recvErrChan <- recvRes{res, nil}
		}
	}()

	sendErrChan := make(chan error)
	go func() {
		if err := c.send(ctx, conn, EndpointRecv, req, stream); err != nil {
			sendErrChan <- err
		} else {
			sendErrChan <- nil
		}
	}()

	var res recvRes
	var sendErr error
	var cause error // one of the above
	didTryClose := false
	for i := 0; i < 2; i++ {
		select {
		case res = <-recvErrChan:
			c.log.WithField("errType", fmt.Sprintf("%T", res.err)).WithError(res.err).Debug("recv goroutine returned")
			if res.err != nil && cause == nil {
				cause = res.err
			}
		case sendErr = <-sendErrChan:
			c.log.WithField("errType", fmt.Sprintf("%T", sendErr)).WithError(sendErr).Debug("send goroutine returned")
			if sendErr != nil && cause == nil {
				cause = sendErr
			}
		}
		if !didTryClose && (res.err != nil || sendErr != nil) {
			didTryClose = true
			if err := conn.Close(); err != nil {
				c.log.WithError(err).Error("ReqRecv: cannot close connection, will likely block indefinitely")
			}
			c.log.WithError(err).Debug("ReqRecv: closed connection, should trigger other goroutine error")
		}
	}

	if !didTryClose {
		// didn't close it in above loop, so we can give it back
		c.putWire(conn)
	}

	// if receive failed with a RemoteHandlerError, we know the transport was not broken
	// => take the remote error as cause for the operation to fail
	// TODO combine errors if send also failed
	//      (after all, send could have crashed on our side, rendering res.err a mere symptom of the cause)
	if _, ok := res.err.(*RemoteHandlerError); ok {
		cause = res.err
	}

	return res.res, cause
}

func (c *Client) ReqPing(ctx context.Context, req *pdu.PingReq) (*pdu.PingRes, error) {
	conn, err := c.getWire(ctx)
	if err != nil {
		return nil, err
	}
	defer c.putWire(conn)

	if err := c.send(ctx, conn, EndpointPing, req, nil); err != nil {
		return nil, err
	}

	var res pdu.PingRes
	if err := c.recv(ctx, conn, &res); err != nil {
		return nil, err
	}

	return &res, nil
}
