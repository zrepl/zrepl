package logging

import "context"

type contextKey int

const (
	contextKeyTraceNode contextKey = 1 + iota
	contextKeyLoggers
	contextKeyInjectedField
)

var contextKeys = []contextKey{
	contextKeyTraceNode,
	contextKeyLoggers,
	contextKeyInjectedField,
}

func WithInherit(ctx, inheritFrom context.Context) context.Context {
	for _, k := range contextKeys {
		if v := inheritFrom.Value(k); v != nil {
			ctx = context.WithValue(ctx, k, v) // no shadow
		}
	}
	return ctx
}
