package circlog

import (
	"fmt"
	"math/bits"
	"sync"
)

const CIRCULARLOG_INIT_SIZE int = 32 << 10

type CircularLog struct {
	buf         []byte
	size        int
	max         int
	written     int
	writeCursor int
	/*
		Mutex prevents:
		concurrent writes:
			buf, size, written, writeCursor in Write([]byte)
			buf, writeCursor in Reset()
		data races vs concurrent Write([]byte) calls:
			size in Size()
			size, writeCursor in Len()
			buf, size, written, writeCursor in Bytes()
	*/
	mtx sync.Mutex
}

func MustNewCircularLog(max int) *CircularLog {
	log, err := NewCircularLog(max)
	if err != nil {
		panic(err)
	}
	return log
}

func NewCircularLog(max int) (*CircularLog, error) {
	if max <= 0 {
		return nil, fmt.Errorf("max must be positive")
	}

	return &CircularLog{
		size: CIRCULARLOG_INIT_SIZE,
		buf:  make([]byte, CIRCULARLOG_INIT_SIZE),
		max:  max,
	}, nil
}

func nextPow2Int(n int) int {
	if n < 1 {
		panic("can only round up positive integers")
	}
	r := uint(n)
	// Credit: https://graphics.stanford.edu/~seander/bithacks.html#RoundUpPowerOf2
	r--
	// Assume at least a 32 bit integer
	r |= r >> 1
	r |= r >> 2
	r |= r >> 4
	r |= r >> 8
	r |= r >> 16
	if bits.UintSize == 64 {
		r |= r >> 32
	}
	r++
	// Can't exceed max positive int value
	if r > ^uint(0)>>1 {
		panic("rounded to larger than int()")
	}
	return int(r)
}

func (cl *CircularLog) Write(data []byte) (int, error) {
	cl.mtx.Lock()
	defer cl.mtx.Unlock()
	n := len(data)

	// Keep growing the buffer by doubling until
	// hitting cl.max
	bufAvail := cl.size - cl.writeCursor
	// If cl.writeCursor wrapped on the last write,
	// then the buffer is full, not empty.
	if cl.writeCursor == 0 && cl.written > 0 {
		bufAvail = 0
	}
	if n > bufAvail && cl.size < cl.max {
		// Add to size, not writeCursor, so as
		// to not resize multiple times if this
		// Write() immediately fills up the buffer
		newSize := nextPow2Int(cl.size + n)
		if newSize > cl.max {
			newSize = cl.max
		}
		newBuf := make([]byte, newSize)
		// Reset write cursor to old size if wrapped
		if cl.writeCursor == 0 && cl.written > 0 {
			cl.writeCursor = cl.size
		}
		copy(newBuf, cl.buf[:cl.writeCursor])
		cl.buf = newBuf
		cl.size = newSize
	}

	// If data to be written is larger than the max size,
	// discard all but the last cl.size bytes
	if n > cl.size {
		data = data[n-cl.size:]
		// Overwrite the beginning of data
		// with a string indicating truncation
		copy(data, []byte("(...)"))
	}
	// First copy data to the end of buf. If that wasn't
	// all of data, then copy the rest to the beginning
	// of buf.
	copied := copy(cl.buf[cl.writeCursor:], data)
	if copied < n {
		copied += copy(cl.buf, data[copied:])
	}

	cl.writeCursor = ((cl.writeCursor + copied) % cl.size)
	cl.written += copied
	return copied, nil
}

func (cl *CircularLog) Size() int {
	cl.mtx.Lock()
	defer cl.mtx.Unlock()
	return cl.size
}

func (cl *CircularLog) Len() int {
	cl.mtx.Lock()
	defer cl.mtx.Unlock()
	if cl.written >= cl.size {
		return cl.size
	} else {
		return cl.writeCursor
	}
}

func (cl *CircularLog) TotalWritten() int {
	cl.mtx.Lock()
	defer cl.mtx.Unlock()

	return cl.written
}

func (cl *CircularLog) Reset() {
	cl.mtx.Lock()
	defer cl.mtx.Unlock()
	cl.writeCursor = 0
	cl.buf = make([]byte, CIRCULARLOG_INIT_SIZE)
	cl.size = CIRCULARLOG_INIT_SIZE
}

func (cl *CircularLog) Bytes() []byte {
	cl.mtx.Lock()
	defer cl.mtx.Unlock()
	switch {
	case cl.written >= cl.size && cl.writeCursor == 0:
		return cl.buf
	case cl.written > cl.size:
		ret := make([]byte, cl.size)
		copy(ret, cl.buf[cl.writeCursor:])
		copy(ret[cl.size-cl.writeCursor:], cl.buf[:cl.writeCursor])
		return ret
	default:
		return cl.buf[:cl.writeCursor]
	}
}

func (cl *CircularLog) String() string {
	return string(cl.Bytes())
}
