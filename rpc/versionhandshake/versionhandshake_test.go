package versionhandshake

import (
	"bytes"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zrepl/zrepl/util/socketpair"
	"io"
	"strings"
	"testing"
	"time"
)

func TestHandshakeMessage_Encode(t *testing.T) {

	msg := HandshakeMessage{
		ProtocolVersion: 2342,
	}

	encB, err := msg.Encode()
	require.NoError(t, err)
	enc := string(encB)
	t.Logf("enc: %s", enc)



	assert.False(t, strings.ContainsAny(enc[0:10], " "))
	assert.True(t, enc[10] == ' ')

	var (
		headerlen, protoversion, extensionCount int
	)
	n, err := fmt.Sscanf(enc, "%010d ZREPL_ZFS_REPLICATION PROTOVERSION=%04d EXTENSIONS=%04d\n",
		&headerlen, &protoversion, &extensionCount)
	if n != 3 || (err != nil && err != io.EOF) {
		t.Fatalf("%v %v", n, err)
	}

	assert.Equal(t, 2342, protoversion)
	assert.Equal(t, 0, extensionCount)
	assert.Equal(t, len(enc)-11, headerlen)

}

func TestHandshakeMessage_Encode_InvalidProtocolVersion(t *testing.T) {

	for _, pv := range []int{-1, 0,  10000, 10001} {
		t.Logf("testing invalid protocol version = %v", pv)
		msg := HandshakeMessage{
			ProtocolVersion: pv,
		}
		b, err := msg.Encode()
		assert.Error(t, err)
		assert.Nil(t, b)
	}

}

func TestHandshakeMessage_DecodeReader(t *testing.T) {

	in := HandshakeMessage{
		2342,
		[]string{"foo", "bar 2342"},
	}

	enc, err := in.Encode()
	require.NoError(t, err)

	out := HandshakeMessage{}
	err = out.DecodeReader(bytes.NewReader([]byte(enc)), 4 * 4096)
	assert.NoError(t, err)
	assert.Equal(t, 2342, out.ProtocolVersion)
	assert.Equal(t, 2, len(out.Extensions))
	assert.Equal(t, "foo", out.Extensions[0])
	assert.Equal(t, "bar 2342", out.Extensions[1])

}

func TestDoHandshakeVersion_ErrorOnDifferentVersions(t *testing.T) {
	srv, client, err := socketpair.SocketPair()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	defer client.Close()

	srvErrCh := make(chan error)
	go func() {
		srvErrCh <- DoHandshakeVersion(srv, time.Now().Add(2*time.Second), 1)
	}()
	err = DoHandshakeVersion(client, time.Now().Add(2*time.Second), 2)
	t.Log(err)
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "version"))

	srvErr := <-srvErrCh
	t.Log(srvErr)
	assert.Error(t, srvErr)
	assert.True(t, strings.Contains(srvErr.Error(), "version"))
}

func TestDoHandshakeCurrentVersion(t *testing.T) {
	srv, client, err := socketpair.SocketPair()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	defer client.Close()

	srvErrCh := make(chan error)
	go func() {
		srvErrCh <- DoHandshakeVersion(srv, time.Now().Add(2*time.Second), 1)
	}()
	err = DoHandshakeVersion(client, time.Now().Add(2*time.Second), 1)
	assert.Nil(t, err)
	assert.Nil(t, <-srvErrCh)

}
