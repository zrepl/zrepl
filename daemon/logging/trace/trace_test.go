package trace

import (
	"context"
	"fmt"
	"testing"

	"github.com/gitchander/permutation"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegularSpanUsage(t *testing.T) {
	root, endRoot := WithTask(context.Background(), "root")
	defer endRoot()

	s1, endS1 := WithSpan(root, "parent")
	s2, endS2 := WithSpan(s1, "child")
	_, endS3 := WithSpan(s2, "grand-child")
	require.NotPanics(t, func() { endS3() })
	require.NotPanics(t, func() { endS2() })

	// reuse
	_, endS4 := WithSpan(s1, "child-2")
	require.NotPanics(t, func() { endS4() })

	// close parent
	require.NotPanics(t, func() { endS1() })
}

func TestMultipleActiveChildSpansNotAllowed(t *testing.T) {
	root, endRoot := WithTask(context.Background(), "root")
	defer endRoot()

	s1, _ := WithSpan(root, "s1")
	_, endS2 := WithSpan(s1, "s1-child1")

	require.PanicsWithValue(t, ErrAlreadyActiveChildSpan, func() {
		_, _ = WithSpan(s1, "s1-child2")
	})

	endS2()

	require.NotPanics(t, func() {
		_, _ = WithSpan(s1, "s1-child2")
	})
}

func TestForkingChildSpansNotAllowed(t *testing.T) {
	root, endRoot := WithTask(context.Background(), "root")
	defer endRoot()

	s1, _ := WithSpan(root, "s1")
	sc, endSC := WithSpan(s1, "s1-child")
	_, _ = WithSpan(sc, "s1-child-child")

	require.PanicsWithValue(t, ErrSpanStillHasActiveChildSpan, func() {
		endSC()
	})
}

func TestRegularTaskUsage(t *testing.T) {
	// assert concurrent activities on different tasks can end in any order
	closeOrder := []int{0, 1, 2}
	closeOrders := permutation.New(permutation.IntSlice(closeOrder))
	for closeOrders.Next() {
		t.Run(fmt.Sprintf("%v", closeOrder), func(t *testing.T) {
			root, endRoot := WithTask(context.Background(), "root")
			defer endRoot()

			c1, endC1 := WithTask(root, "c1")
			defer endC1()
			c2, endC2 := WithTask(root, "c2")
			defer endC2()

			// begin 3 concurrent activities
			_, endAR := WithSpan(root, "aR")
			_, endAC1 := WithSpan(c1, "aC1")
			_, endAC2 := WithSpan(c2, "aC2")

			endFuncs := []DoneFunc{endAR, endAC1, endAC2}
			for _, i := range closeOrder {
				require.NotPanics(t, func() {
					endFuncs[i]()
				}, "%v", i)
			}
		})
	}
}

func TestTaskEndWithActiveChildTaskNotAllowed(t *testing.T) {
	root, _ := WithTask(context.Background(), "root")
	c, endC := WithTask(root, "child")
	_, _ = WithTask(c, "grand-child")
	func() {
		defer func() {
			r := recover()
			require.NotNil(t, r)
			err, ok := r.(error)
			require.True(t, ok)
			require.Equal(t, ErrTaskStillHasActiveChildTasks, errors.Cause(err))
		}()
		endC()
	}()

}

func TestIdempotentEndTask(t *testing.T) {
	_, end := WithTask(context.Background(), "root")
	end()
	require.NotPanics(t, func() { end() })
}

func TestCannotReuseEndedTask(t *testing.T) {
	root, end := WithTask(context.Background(), "root")
	end()
	require.PanicsWithValue(t, ErrParentTaskAlreadyEnded, func() { WithTask(root, "child-after-parent-ended") })
}

func TestSpansPanicIfNoParentTask(t *testing.T) {
	require.Panics(t, func() { WithSpan(context.Background(), "taskless-span") })
}

func TestIdempotentEndSpan(t *testing.T) {
	root, _ := WithTask(context.Background(), "root")
	_, end := WithSpan(root, "span")
	end()
	require.NotPanics(t, func() { end() })
}

func logAndGetTraceNode(t *testing.T, descr string, ctx context.Context) *traceNode {
	n, ok := ctx.Value(contextKeyTraceNode).(*traceNode)
	require.True(t, ok)
	t.Logf("% 20s %p %#v", descr, n, n)
	return n
}

func TestWhiteboxHierachy(t *testing.T) {
	root, e1 := WithTask(context.Background(), "root")
	rootN := logAndGetTraceNode(t, "root", root)
	assert.Nil(t, rootN.parentTask)
	assert.Nil(t, rootN.parentSpan)

	child, e2 := WithSpan(root, "child")
	childN := logAndGetTraceNode(t, "child", child)
	assert.Equal(t, rootN, childN.parentTask)
	assert.Equal(t, rootN, childN.parentSpan)

	grandchild, e3 := WithSpan(child, "grandchild")
	grandchildN := logAndGetTraceNode(t, "grandchild", grandchild)
	assert.Equal(t, rootN, grandchildN.parentTask)
	assert.Equal(t, childN, grandchildN.parentSpan)

	gcTask, e4 := WithTask(grandchild, "grandchild-task")
	gcTaskN := logAndGetTraceNode(t, "grandchild-task", gcTask)
	assert.Equal(t, rootN, gcTaskN.parentTask)
	assert.Nil(t, gcTaskN.parentSpan)

	// it is allowed that a child task outlives the _span_ in which it was created
	// (albeit not its parent task)
	e3()
	e2()
	gcTaskSpan, e5 := WithSpan(gcTask, "granschild-task-span")
	gcTaskSpanN := logAndGetTraceNode(t, "granschild-task-span", gcTaskSpan)
	assert.Equal(t, gcTaskN, gcTaskSpanN.parentTask)
	assert.Equal(t, gcTaskN, gcTaskSpanN.parentSpan)
	e5()

	e4()
	e1()
}
