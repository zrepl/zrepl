package tracing

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIt(t *testing.T) {
	ctx := context.Background()

	ctx = Child(ctx, "a")
	ctx = Child(ctx, "b")
	ctx = Child(ctx, "c")
	ctx = Child(ctx, "d")

	assert.Equal(t, "dcba", strings.Join(GetStack(ctx), ""))

}
