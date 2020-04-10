package chainedio

import "io"

type ChainedReadCloser struct {
	readers   []io.Reader
	curReader int
}

func NewChainedReader(reader ...io.Reader) *ChainedReadCloser {
	return &ChainedReadCloser{
		readers:   reader,
		curReader: 0,
	}
}

func (c *ChainedReadCloser) Read(buf []byte) (n int, err error) {

	n = 0

	for c.curReader < len(c.readers) {
		n, err = c.readers[c.curReader].Read(buf)
		if err == io.EOF {
			c.curReader++
			continue
		}
		break
	}
	if c.curReader == len(c.readers) {
		err = io.EOF // actually, there was no gap
	}

	return
}

func (c *ChainedReadCloser) Close() error {
	for _, r := range c.readers {
		if c, ok := r.(io.Closer); ok {
			c.Close() // TODO debug log error?
		}
	}
	return nil
}
