// Package transportmux wraps a transport.{Connecter,AuthenticatedListener}
// to distinguish different connection types based on a label
// sent from client to server on connection establishment.
//
// Labels are plain text and fixed length.
package transportmux

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/transport"
)

type contextKey int

const (
	contextKeyLog contextKey = 1 + iota
)

type Logger = logger.Logger

func WithLogger(ctx context.Context, log Logger) context.Context {
	return context.WithValue(ctx, contextKeyLog, log)
}

func getLog(ctx context.Context) Logger {
	if l, ok := ctx.Value(contextKeyLog).(Logger); ok {
		return l
	}
	return logger.NewNullLogger()
}

type acceptRes struct {
	conn *transport.AuthConn
	err  error
}

type demuxListener struct {
	conns chan acceptRes
}

func (l *demuxListener) Accept(ctx context.Context) (*transport.AuthConn, error) {
	res := <-l.conns
	return res.conn, res.err
}

type demuxAddr struct {}

func (demuxAddr) Network() string { return "demux" }
func (demuxAddr) String() string { return "demux" }

func (l *demuxListener) Addr() net.Addr {
	return demuxAddr{}
}

func (l *demuxListener) Close() error { return nil } // TODO

// Exact length of a label in bytes (0-byte padded if it is shorter).
// This is a protocol constant, changing it breaks the wire protocol.
const LabelLen = 64

func padLabel(out []byte, label string) (error) {
	if len(label) > LabelLen {
		return fmt.Errorf("label %q exceeds max length (is %d, max %d)", label, len(label), LabelLen)
	}
	if len(out) != LabelLen {
		panic(fmt.Sprintf("implementation error: %d", out))
	}
	labelBytes := []byte(label)
	copy(out[:], labelBytes)
	return nil
}

func Demux(ctx context.Context, rawListener transport.AuthenticatedListener, labels []string, timeout time.Duration) (map[string]transport.AuthenticatedListener, error) {

	padded := make(map[[64]byte]*demuxListener, len(labels))
	ret := make(map[string]transport.AuthenticatedListener, len(labels))
	for _, label := range labels {
		var labelPadded [LabelLen]byte
		err := padLabel(labelPadded[:], label)
		if err != nil {
			return nil, err
		}
		if _, ok := padded[labelPadded]; ok {
			return nil, fmt.Errorf("duplicate label %q", label)
		}
		dl := &demuxListener{make(chan acceptRes)}
		padded[labelPadded] = dl
		ret[label] = dl
	}

	// invariant: padded contains same-length, non-duplicate labels

	go func() {
		<-ctx.Done()
		getLog(ctx).Debug("context cancelled, closing listener")
		if err := rawListener.Close(); err != nil {
			getLog(ctx).WithError(err).Error("error closing listener")
		}
	}()

	go func() {
		for {
			rawConn, err := rawListener.Accept(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				getLog(ctx).WithError(err).WithField("errType", fmt.Sprintf("%T", err)).Error("accept error")
				continue
			}
			closeConn := func() {
				if err := rawConn.Close(); err != nil {
					getLog(ctx).WithError(err).Error("cannot close conn")
				}
			}

			if err := rawConn.SetDeadline(time.Now().Add(timeout)); err != nil {
				getLog(ctx).WithError(err).Error("SetDeadline failed")
				closeConn()
				continue
			}

			var labelBuf [LabelLen]byte
			if _, err := io.ReadFull(rawConn, labelBuf[:]); err != nil {
				getLog(ctx).WithError(err).Error("error reading label")
				closeConn()
				continue
			}

			demuxListener, ok := padded[labelBuf]
			if !ok {
				getLog(ctx).WithError(err).
					WithField("client_label", fmt.Sprintf("%q", labelBuf)).
					Error("unknown client label")
				closeConn()
				continue
			}

			rawConn.SetDeadline(time.Time{})
			// blocking is intentional
			demuxListener.conns <- acceptRes{conn: rawConn, err: nil}
		}
	}()

	return ret, nil
}

type labeledConnecter struct {
	label []byte
	transport.Connecter	
}

func (c labeledConnecter) Connect(ctx context.Context) (transport.Wire, error) {
	conn, err := c.Connecter.Connect(ctx)
	if err != nil {
		return nil, err
	}
	closeConn := func(why error) {
		getLog(ctx).WithField("reason", why.Error()).Debug("closing connection")
		if err := conn.Close(); err != nil {
			getLog(ctx).WithError(err).Error("error closing connection after label write error")
		}
	}

	if dl, ok := ctx.Deadline(); ok {
		defer conn.SetDeadline(time.Time{})
		if err := conn.SetDeadline(dl); err != nil {
			closeConn(err)
			return nil, err
		}
	}
	n, err := conn.Write(c.label)
	if err != nil {
		closeConn(err)
		return nil, err
	}
	if n != len(c.label) {
		closeConn(fmt.Errorf("short label write"))
		return nil, io.ErrShortWrite
	}
	return conn, nil
}

func MuxConnecter(rawConnecter transport.Connecter, labels []string, timeout time.Duration) (map[string]transport.Connecter, error) {
	ret := make(map[string]transport.Connecter, len(labels))
	for _, label := range labels {
		var paddedLabel [LabelLen]byte
		if err := padLabel(paddedLabel[:], label); err != nil {
			return nil, err
		}
		lc := &labeledConnecter{paddedLabel[:], rawConnecter}
		if _, ok := ret[label]; ok {
			return nil, fmt.Errorf("duplicate label %q", label)
		}
		ret[label] = lc
	}
	return ret, nil
}

