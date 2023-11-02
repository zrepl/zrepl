package main

import (
	"bytes"
	"io"
	"log"

	"github.com/zrepl/zrepl/transport"
)

type CloseWriteMode uint

const (
	CloseWriteClientSide CloseWriteMode = 1 + iota
	CloseWriteServerSide
)

type CloseWrite struct {
	mode CloseWriteMode
}

// sent repeatedly
var closeWriteTestSendData = bytes.Repeat([]byte{0x23, 0x42}, 1<<24)
var closeWriteErrorMsg = []byte{0xb, 0xa, 0xd, 0xf, 0x0, 0x0, 0xd}

func (m CloseWrite) Client(wire transport.Wire) {
	switch m.mode {
	case CloseWriteClientSide:
		m.receiver(wire)
	case CloseWriteServerSide:
		m.sender(wire)
	default:
		panic(m.mode)
	}
}

func (m CloseWrite) Server(wire transport.Wire) {
	switch m.mode {
	case CloseWriteClientSide:
		m.sender(wire)
	case CloseWriteServerSide:
		m.receiver(wire)
	default:
		panic(m.mode)
	}
}

func (CloseWrite) sender(wire transport.Wire) {
	defer func() {
		closeErr := wire.Close()
		log.Printf("closeErr=%T %s", closeErr, closeErr)
	}()

	writeDone := make(chan struct{}, 1)
	go func() {
		close(writeDone)
		for {
			_, err := wire.Write(closeWriteTestSendData)
			if err != nil {
				return
			}
		}
	}()

	defer func() {
		<-writeDone
	}()

	var respBuf bytes.Buffer
	_, err := io.Copy(&respBuf, wire)
	if err != nil {
		log.Fatalf("should have received io.EOF, which is masked by io.Copy, got: %s", err)
	}
	if !bytes.Equal(respBuf.Bytes(), closeWriteErrorMsg) {
		log.Fatalf("did not receive error message, got response with len %v:\n%v", respBuf.Len(), respBuf.Bytes())
	}
}

func (CloseWrite) receiver(wire transport.Wire) {
	// consume half the test data, then detect an error, send it and CloseWrite

	r := io.LimitReader(wire, int64(5*len(closeWriteTestSendData)/3))
	_, err := io.Copy(io.Discard, r)
	noerror(err)

	var errBuf bytes.Buffer
	errBuf.Write(closeWriteErrorMsg)
	_, err = io.Copy(wire, &errBuf)
	noerror(err)

	err = wire.CloseWrite()
	noerror(err)

	// drain wire, as documented in transport.Wire, this is the only way we know the client closed the conn
	_, err = io.Copy(io.Discard, wire)
	if err != nil {
		// io.Copy masks io.EOF to nil, and we expect io.EOF from the client's Close() call
		log.Panicf("unexpected error returned from reading conn: %s", err)
	}

	closeErr := wire.Close()
	log.Printf("closeErr=%T %s", closeErr, closeErr)

}
