package logging

import "context"

type contextKey int

const (
	contextKeyLoggers contextKey = 1 + iota
	contextKeyInjectedField
)

var contextKeys = []contextKey{
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
