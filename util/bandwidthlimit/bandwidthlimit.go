package bandwidthlimit

import (
	"errors"
	"io"

	"github.com/juju/ratelimit"
)

type Wrapper interface {
	WrapReadCloser(io.ReadCloser) io.ReadCloser
}

type Config struct {
	// Units in this struct are in _bytes_.

	Max            int64 // < 0 means no limit, BucketCapacity is irrelevant then
	BucketCapacity int64
}

func ValidateConfig(conf Config) error {
	if conf.BucketCapacity == 0 {
		return errors.New("BucketCapacity must not be zero")
	}
	return nil
}

func WrapperFromConfig(conf Config) Wrapper {
	if err := ValidateConfig(conf); err != nil {
		panic(err)
	}

	if conf.Max < 0 {
		return noLimit{}
	}

	return &withLimit{
		bucket: ratelimit.NewBucketWithRate(float64(conf.Max), conf.BucketCapacity),
	}
}

type noLimit struct{}

func (_ noLimit) WrapReadCloser(rc io.ReadCloser) io.ReadCloser { return rc }

type withLimit struct {
	bucket *ratelimit.Bucket
}

func (l *withLimit) WrapReadCloser(rc io.ReadCloser) io.ReadCloser {
	return WrapReadCloser(rc, l.bucket)
}

type withLimitReadCloser struct {
	orig    io.Closer
	limited io.Reader
}

func (r *withLimitReadCloser) Read(buf []byte) (int, error) {
	return r.limited.Read(buf)
}

func (r *withLimitReadCloser) Close() error {
	return r.orig.Close()
}

func WrapReadCloser(rc io.ReadCloser, bucket *ratelimit.Bucket) io.ReadCloser {
	return &withLimitReadCloser{
		limited: ratelimit.Reader(rc, bucket),
		orig:    rc,
	}
}
