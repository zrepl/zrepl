package tracing

import "context"

type tracingContextKey int

const (
	CallerContext tracingContextKey = 1 + iota
)

type jobSubtree struct {
	jobid string
}

type ctx struct {
	parent *ctx
	job    *jobSubtree
	ident  string
}

var root = &ctx{nil, nil, ""}

func getParentOrRoot(c context.Context) *ctx {
	parent, ok := c.Value(CallerContext).(*ctx)
	if !ok {
		parent = root
	}
	return parent
}

func makeChild(c context.Context, child *ctx) context.Context {
	if child.parent == nil {
		panic(child)
	}
	return context.WithValue(c, CallerContext, child)
}

func Child(c context.Context, ident string) context.Context {
	parent := getParentOrRoot(c)
	return makeChild(c, &ctx{parent: parent, ident: ident})
}

func GetStack(c context.Context) (idents []string) {
	ct, ok := c.Value(CallerContext).(*ctx)
	if !ok {
		return idents
	}
	for ct.parent != nil {
		idents = append(idents, ct.ident)
		ct = ct.parent
	}
	return idents
}
