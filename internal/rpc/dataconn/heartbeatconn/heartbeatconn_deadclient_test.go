package heartbeatconn_test

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/LyingCak3/zrepl/internal/rpc/dataconn/heartbeatconn"
	"github.com/LyingCak3/zrepl/internal/util/socketpair"
)

// Test behavior of heartbeatconn when the client is dead.
//
// Test strategy is to have a proxy between two heartbeatconn.Conn instances,
// set up working heartbeatconn instances on both sides, then stop the proxy.
func TestHeartbeatconnDeadClient(t *testing.T) {
	var wg sync.WaitGroup
	defer wg.Wait()

	a, b, err := socketpair.SocketPair()
	require.NoError(t, err)
	c, d, err := socketpair.SocketPair()
	require.NoError(t, err)

	var stopProxy atomic.Bool
	proxy := func(src, dst *net.UnixConn, done chan struct{}) {
		defer wg.Done()
		defer close(done)
		defer t.Log("proxy exiting")
		buf := make([]byte, 1024)
		for stopProxy.Load() == false {
			n, err := src.Read(buf)
			require.NoError(t, err)
			t.Logf("proxy read %d bytes", n)
			for i := 0; i < n; {
				nwritten, err := dst.Write(buf[i:n])
				require.NoError(t, err)
				i += nwritten
			}
		}
	}
	wg.Add(1)
	proxyBC := make(chan struct{})
	go proxy(b, c, proxyBC)
	wg.Add(1)
	proxyCB := make(chan struct{})
	go proxy(c, b, proxyCB)

	const heartbeatInterval = 100 * time.Millisecond
	const heartbeatTimeout = 10 * heartbeatInterval
	aHc := heartbeatconn.Wrap(a, heartbeatInterval, heartbeatTimeout)
	defer aHc.Shutdown()
	dHc := heartbeatconn.Wrap(d, heartbeatInterval, heartbeatTimeout)
	defer dHc.Shutdown()

	// follow API requirements to always ReadFrame
	aOut := make(chan net.Error, 1)
	dOut := make(chan net.Error, 1)
	readFrame := func(conn *heartbeatconn.Conn, out chan net.Error) {
		defer wg.Done()
		_, err := conn.ReadFrame()
		require.Error(t, err, "%T %s\n\n%#v", err, err, err)
		netErr, ok := err.(net.Error)
		require.True(t, ok)
		out <- netErr
	}
	wg.Add(1)
	go readFrame(aHc, aOut)
	wg.Add(1)
	go readFrame(dHc, dOut)

	time.Sleep(30 * heartbeatInterval)

	t.Logf("stop proxy")
	stopProxy.Store(true)
	<-proxyBC
	<-proxyCB
	// heartbeatconn should fail ReadFrame within heartbeatTimeout + scheduler delay
	const slop = 10 * time.Millisecond
	waitStart := time.Now()
	var aErr net.Error = nil
	var dErr net.Error = nil
	for aErr == nil || dErr == nil {
		select {
		case aErr = <-aOut:
			t.Logf("aErr: %s", aErr)
		case dErr = <-dOut:
			t.Logf("dErr: %s", dErr)
		}
	}
	waitTime := time.Since(waitStart)

	// assert timeline unblock of ReadFrame()
	require.True(t, waitTime > heartbeatTimeout-slop, "waitTime=%s", waitTime)
	require.True(t, waitTime < heartbeatTimeout+slop, "waitTime=%s", waitTime)

	// assert the error is Timeout(), so zrepl replication driver makes a new attempt
	require.True(t, aErr.Timeout())
	require.True(t, dErr.Timeout())
}
