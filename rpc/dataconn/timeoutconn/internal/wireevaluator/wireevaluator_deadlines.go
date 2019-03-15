package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"github.com/zrepl/zrepl/transport"
)

type DeadlineMode uint

const (
	DeadlineModeClientTimeout DeadlineMode = 1 + iota
	DeadlineModeServerTimeout
)

type Deadlines struct {
	mode DeadlineMode
}

func (d Deadlines) Client(wire transport.Wire) {
	switch d.mode {
	case DeadlineModeClientTimeout:
		d.sleepThenSend(wire)
	case DeadlineModeServerTimeout:
		d.sendThenRead(wire)
	default:
		panic(d.mode)
	}
}

func (d Deadlines) Server(wire transport.Wire) {
	switch d.mode {
	case DeadlineModeClientTimeout:
		d.sendThenRead(wire)
	case DeadlineModeServerTimeout:
		d.sleepThenSend(wire)
	default:
		panic(d.mode)
	}
}

var deadlinesTimeout = 1 * time.Second

func (d Deadlines) sleepThenSend(wire transport.Wire) {
	defer wire.Close()

	log.Print("sleepThenSend")

	// exceed timeout of peer (do not respond to their hi msg)
	time.Sleep(3 * deadlinesTimeout)
	// expect that the client has hung up on us by now
	err := d.sendMsg(wire, "hi")
	log.Printf("err=%s", err)
	log.Printf("err=%#v", err)
	if err == nil {
		log.Panic("no error")
	}
	if _, ok := err.(net.Error); !ok {
		log.Panic("not a net error")
	}

}

func (d Deadlines) sendThenRead(wire transport.Wire) {

	log.Print("sendThenRead")

	err := d.sendMsg(wire, "hi")
	noerror(err)

	err = wire.SetReadDeadline(time.Now().Add(deadlinesTimeout))
	noerror(err)

	m, err := d.recvMsg(wire)
	log.Printf("m=%q", m)
	log.Printf("err=%s", err)
	log.Printf("err=%#v", err)

	// close asap so that the peer get's a 'connection reset by peer' error or similar
	closeErr := wire.Close()
	if closeErr != nil {
		panic(closeErr)
	}

	var neterr net.Error
	var ok bool
	if err == nil {
		goto unexpErr // works for nil, too
	}
	neterr, ok = err.(net.Error)
	if !ok {
		log.Println("not a net error")
		goto unexpErr
	}
	if !neterr.Timeout() {
		log.Println("not a timeout")
	}

	return

unexpErr:
	panic(fmt.Sprintf("sendThenRead: client should have hung up but got error %T %s", err, err))
}

const deadlinesMsgLen = 40

func (d Deadlines) sendMsg(wire transport.Wire, msg string) error {
	if len(msg) > deadlinesMsgLen {
		panic(len(msg))
	}
	var buf [deadlinesMsgLen]byte
	copy(buf[:], []byte(msg))
	n, err := wire.Write(buf[:])
	if err != nil {
		return err
	}
	if n != len(buf) {
		panic("short write not allowed")
	}
	return nil
}

func (d Deadlines) recvMsg(wire transport.Wire) (string, error) {

	var buf bytes.Buffer
	r := io.LimitReader(wire, deadlinesMsgLen)
	_, err := io.Copy(&buf, r)
	if err != nil {
		return "", err
	}
	return buf.String(), nil

}
