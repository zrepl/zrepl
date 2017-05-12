package util

import (
	"io"
)

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
