package rpc

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"github.com/zrepl/zrepl/util"
	"io"
	"strings"
	"testing"
)

func TestByteStreamRPCDecodeJSONError(t *testing.T) {

	r := strings.NewReader("{'a':'aber'}")

	var chunked bytes.Buffer
	ch := util.NewChunker(r)
	io.Copy(&chunked, &ch)

	type SampleType struct {
		A uint
	}
	var s SampleType
	err := readChunkedJSON(&chunked, &s)
	assert.NotNil(t, err)

	_, ok := err.(ByteStreamRPCDecodeJSONError)
	if !ok {
		t.Errorf("expected ByteStreamRPCDecodeJSONError, got %t\n", err)
		t.Errorf("%s\n", err)
	}

}
