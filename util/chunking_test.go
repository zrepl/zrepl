package util

import (
	"bytes"
	"encoding/binary"
	"io"
	"reflect"
	"testing"
	"testing/quick"

	"github.com/stretchr/testify/assert"
)

func TestUnchunker(t *testing.T) {

	buf := bytes.Buffer{}
	binary.Write(&buf, ChunkHeaderByteOrder, uint32(2))
	buf.WriteByte(0xca)
	buf.WriteByte(0xfe)
	binary.Write(&buf, ChunkHeaderByteOrder, uint32(0))
	buf.WriteByte(0xff) // sentinel, should not be read

	un := NewUnchunker(&buf)

	recv := bytes.Buffer{}
	n, err := io.Copy(&recv, un)
	assert.Nil(t, err)
	assert.Equal(t, int64(2), n)
	assert.Equal(t, []byte{0xca, 0xfe}, recv.Bytes())

}

func TestChunker(t *testing.T) {

	buf := bytes.Buffer{}
	buf.WriteByte(0xca)
	buf.WriteByte(0xfe)

	ch := NewChunker(&buf)

	chunked := bytes.Buffer{}
	n, err := io.Copy(&chunked, &ch)
	assert.Nil(t, err)
	assert.Equal(t, int64(4+2+4), n)
	assert.Equal(t, []byte{0x2, 0x0, 0x0, 0x0, 0xca, 0xfe, 0x0, 0x0, 0x0, 0x0}, chunked.Bytes())

}

func TestUnchunkerUnchunksChunker(t *testing.T) {

	f := func(b []byte) bool {

		buf := bytes.NewBuffer(b)
		ch := NewChunker(buf)
		unch := NewUnchunker(&ch)
		var tx bytes.Buffer
		_, err := io.Copy(&tx, unch)
		if err != nil {
			return false
		}

		return reflect.DeepEqual(b, tx.Bytes())
	}

	cfg := quick.Config{
		MaxCount:      3 * int(ChunkBufSize),
		MaxCountScale: 2.0,
	}

	if err := quick.Check(f, &cfg); err != nil {
		t.Error(err)
	}

}
