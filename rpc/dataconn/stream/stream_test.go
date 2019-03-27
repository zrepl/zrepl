package stream

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/rpc/dataconn/heartbeatconn"
	"github.com/zrepl/zrepl/util/socketpair"
)

func TestFrameTypesOk(t *testing.T) {
	t.Logf("%v", End)
	assert.True(t, heartbeatconn.IsPublicFrameType(End))
	assert.True(t, heartbeatconn.IsPublicFrameType(StreamErrTrailer))
}

func TestStreamer(t *testing.T) {

	anc, bnc, err := socketpair.SocketPair()
	require.NoError(t, err)

	hto := 1 * time.Hour
	a := heartbeatconn.Wrap(anc, hto, hto)
	b := heartbeatconn.Wrap(bnc, hto, hto)

	log := logger.NewStderrDebugLogger()
	ctx := WithLogger(context.Background(), log)

	stype := uint32(0x23)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		var buf bytes.Buffer
		buf.Write(
			bytes.Repeat([]byte{1, 2}, 1<<25),
		)
		writeStream(ctx, a, &buf, stype)
		log.Debug("WriteStream returned")
		a.Shutdown()
	}()

	go func() {
		defer wg.Done()
		var buf bytes.Buffer
		ch := make(chan readFrameResult, 5)
		wg.Add(1)
		go func() {
			defer wg.Done()
			readFrames(ch, b)
		}()
		err := readStream(ch, b, &buf, stype)
		log.WithField("errType", fmt.Sprintf("%T %v", err, err)).Debug("ReadStream returned")
		assert.Nil(t, err)
		expected := bytes.Repeat([]byte{1, 2}, 1<<25)
		assert.True(t, bytes.Equal(expected, buf.Bytes()))
		b.Shutdown()
	}()

	wg.Wait()

}

type errReader struct {
	t       *testing.T
	readErr error
}

func (er errReader) Read(p []byte) (n int, err error) {
	er.t.Logf("errReader.Read called")
	return 0, er.readErr
}

func TestMultiFrameStreamErrTraileror(t *testing.T) {
	anc, bnc, err := socketpair.SocketPair()
	require.NoError(t, err)

	hto := 1 * time.Hour
	a := heartbeatconn.Wrap(anc, hto, hto)
	b := heartbeatconn.Wrap(bnc, hto, hto)

	log := logger.NewStderrDebugLogger()
	ctx := WithLogger(context.Background(), log)

	longErr := fmt.Errorf("an error that definitley spans more than one frame:\n%s", strings.Repeat("a\n", 1<<4))

	stype := uint32(0x23)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		r := errReader{t, longErr}
		writeStream(ctx, a, &r, stype)
		a.Shutdown()
	}()

	go func() {
		defer wg.Done()
		defer b.Shutdown()
		var buf bytes.Buffer
		ch := make(chan readFrameResult, 5)
		wg.Add(1)
		go func() {
			defer wg.Done()
			readFrames(ch, b)
		}()
		err := readStream(ch, b, &buf, stype)
		t.Logf("%s", err)
		require.NotNil(t, err)
		assert.True(t, buf.Len() == 0)
		assert.Equal(t, err.Kind, ReadStreamErrorKindSource)
		receivedErr := err.Err.Error()
		expectedErr := longErr.Error()
		assert.True(t, receivedErr == expectedErr) // builtin Equals is too slow
		if receivedErr != expectedErr {
			t.Logf("lengths: %v %v", len(receivedErr), len(expectedErr))
		}
	}()

	wg.Wait()
}
