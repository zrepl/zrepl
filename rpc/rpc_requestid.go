package rpc

import (
	"context"
	"encoding/base64"
	"strings"

	"github.com/google/uuid"
)

type requestIDGenerator struct{}

func newRequestIDGenerator() *requestIDGenerator {
	return &requestIDGenerator{}
}

type RequestID struct{ s string }

func (r RequestID) String() string { return r.s }

type requestIDContextKey int

const (
	requestIDContextKeyRequestID requestIDContextKey = 1 + iota
)

func (g *requestIDGenerator) newID() RequestID {
	id := uuid.New()
	var buf strings.Builder
	enc := base64.NewEncoder(base64.RawStdEncoding, &buf)
	n, err := enc.Write(id[:])
	if err != nil {
		panic(err)
	} else if n != len(id) {
		panic(n)
	}
	if err := enc.Close(); err != nil {
		panic(err)
	}
	return RequestID{buf.String()}
}

func (g *requestIDGenerator) inject(ctx context.Context) context.Context {
	return context.WithValue(ctx, requestIDContextKeyRequestID, g.newID())
}

func GetRequestID(ctx context.Context) (id RequestID, ok bool) {
	id, ok = ctx.Value(requestIDContextKeyRequestID).(RequestID)
	return id, ok
}

func MustGetRequestID(ctx context.Context) (id RequestID) {
	id, ok := GetRequestID(ctx)
	if !ok {
		panic("calling context expectes request id to bet set")
	}
	return id
}
