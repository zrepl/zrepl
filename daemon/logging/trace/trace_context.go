package trace

import "context"

type contextKey int

const (
	contextKeyTraceNode contextKey = 1 + iota
)

var contextKeys = []contextKey{
	contextKeyTraceNode,
}

// WithInherit inherits the task hierarchy from inheritFrom into ctx.
// The returned context is a child of ctx, but its task and span are those of inheritFrom.
//
// Note that in most use cases, callers most likely want to call WithTask since it will most likely
// be in some sort of connection handler context.
func WithInherit(ctx, inheritFrom context.Context) context.Context {
	for _, k := range contextKeys {
		if v := inheritFrom.Value(k); v != nil {
			ctx = context.WithValue(ctx, k, v) // no shadow
		}
	}
	return ctx
}
