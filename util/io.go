package util

import (
	"io"
	"os"
)

type ReadWriteCloserLogger struct {
	RWC       io.ReadWriteCloser
	ReadFile  *os.File
	WriteFile *os.File
}

func NewReadWriteCloserLogger(rwc io.ReadWriteCloser, readlog, writelog string) (l *ReadWriteCloserLogger, err error) {
	l = &ReadWriteCloserLogger{
		RWC: rwc,
	}
	flags := os.O_CREATE | os.O_WRONLY
	if readlog != "" {
		if l.ReadFile, err = os.OpenFile(readlog, flags, 0600); err != nil {
			return
		}
	}
	if writelog != "" {
		if l.WriteFile, err = os.OpenFile(writelog, flags, 0600); err != nil {
			return
		}
	}
	return
}

func (c *ReadWriteCloserLogger) Read(buf []byte) (n int, err error) {
	n, err = c.RWC.Read(buf)
	if c.WriteFile != nil {
		if _, writeErr := c.ReadFile.Write(buf[0:n]); writeErr != nil {
			panic(writeErr)
		}
	}
	return
}

func (c *ReadWriteCloserLogger) Write(buf []byte) (n int, err error) {
	n, err = c.RWC.Write(buf)
	if c.ReadFile != nil {
		if _, writeErr := c.WriteFile.Write(buf[0:n]); writeErr != nil {
			panic(writeErr)
		}
	}
	return
}
func (c *ReadWriteCloserLogger) Close() (err error) {
	err = c.RWC.Close()
	if err != nil {
		return
	}
	if c.ReadFile != nil {
		if err := c.ReadFile.Close(); err != nil {
			panic(err)
		}
	}
	if c.WriteFile != nil {
		if err := c.WriteFile.Close(); err != nil {
			panic(err)
		}
	}
	return
}

type ChainedReader struct {
	Readers   []io.Reader
	curReader int
}

func NewChainedReader(reader ...io.Reader) *ChainedReader {
	return &ChainedReader{
		Readers:   reader,
		curReader: 0,
	}
}

func (c *ChainedReader) Read(buf []byte) (n int, err error) {

	n = 0

	for c.curReader < len(c.Readers) {
		n, err = c.Readers[c.curReader].Read(buf)
		if err == io.EOF {
			c.curReader++
			continue
		}
		break
	}
	if c.curReader == len(c.Readers) {
		err = io.EOF // actually, there was no gap
	}

	return
}
