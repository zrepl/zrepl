package limitio

import "io"

type readCloser struct {
	read  int64
	limit int64
	r     io.ReadCloser
}

func ReadCloser(rc io.ReadCloser, limit int64) io.ReadCloser {
	return &readCloser{0, limit, rc}
}

var _ io.ReadCloser = (*readCloser)(nil)

func (r *readCloser) Read(b []byte) (int, error) {

	if len(b) == 0 {
		return 0, nil
	}

	if r.read == r.limit {
		return 0, io.EOF
	}

	if r.read+int64(len(b)) >= r.limit {
		b = b[:int(r.limit-r.read)]
	}

	readN, err := r.r.Read(b)
	r.read += int64(readN)
	return readN, err
}

func (r *readCloser) Close() error { return r.r.Close() }
