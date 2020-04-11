package logging

import (
	"context"
	"encoding/base64"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/zrepl/zrepl/util/envconst"
)

var metrics struct {
	activeTasks                          prometheus.Gauge
	uniqueConcurrentTaskNameBitvecLength *prometheus.GaugeVec
}

func init() {
	metrics.activeTasks = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "zrepl",
		Subsystem: "logging",
		Name:      "active_tasks",
		Help:      "number of active (tracing-level) tasks in the daemon",
	})
	metrics.uniqueConcurrentTaskNameBitvecLength = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "zrepl",
		Subsystem: "logging",
		Name:      "unique_concurrent_task_name_bitvec_length",
		Help:      "length of the bitvec used to find unique names for concurrent tasks",
	}, []string{"task_name"})
}

func RegisterMetrics(r prometheus.Registerer) {
	r.MustRegister(metrics.activeTasks)
	r.MustRegister(metrics.uniqueConcurrentTaskNameBitvecLength)
}

type DoneFunc func()

func getMyCallerOrPanic() string {
	pc, _, _, ok := runtime.Caller(2)
	if !ok {
		panic("cannot get caller")
	}
	details := runtime.FuncForPC(pc)
	if ok && details != nil {
		const prefix = "github.com/zrepl/zrepl"
		return strings.TrimPrefix(strings.TrimPrefix(details.Name(), prefix), "/")
	}
	return ""
}

// use like this:
//
//	defer WithSpanFromStackUpdateCtx(&existingCtx)()
//
//
func WithSpanFromStackUpdateCtx(ctx *context.Context) DoneFunc {
	childSpanCtx, end := WithSpan(*ctx, getMyCallerOrPanic())
	*ctx = childSpanCtx
	return end
}

// derive task name from call stack (caller's name)
func WithTaskFromStack(ctx context.Context) (context.Context, DoneFunc) {
	return WithTask(ctx, getMyCallerOrPanic())
}

// derive task name from call stack (caller's name) and update *ctx
// to point to be the child task ctx
func WithTaskFromStackUpdateCtx(ctx *context.Context) DoneFunc {
	child, end := WithTask(*ctx, getMyCallerOrPanic())
	*ctx = child
	return end
}

// create a task and a span within it in one call
func WithTaskAndSpan(ctx context.Context, task string, span string) (context.Context, DoneFunc) {
	ctx, endTask := WithTask(ctx, task)
	ctx, endSpan := WithSpan(ctx, fmt.Sprintf("%s %s", task, span))
	return ctx, func() {
		endSpan()
		endTask()
	}
}

// create a span during which several child tasks are spawned using the `add` function
func WithTaskGroup(ctx context.Context, taskGroup string) (_ context.Context, add func(f func(context.Context)), waitEnd func()) {
	var wg sync.WaitGroup
	ctx, endSpan := WithSpan(ctx, taskGroup)
	add = func(f func(context.Context)) {
		wg.Add(1)
		defer wg.Done()
		ctx, endTask := WithTask(ctx, taskGroup)
		defer endTask()
		f(ctx)
	}
	waitEnd = func() {
		wg.Wait()
		endSpan()
	}
	return ctx, add, waitEnd
}

var taskNamer = newUniqueTaskNamer(metrics.uniqueConcurrentTaskNameBitvecLength)

// Start a new root task or create a child task of an existing task.
//
// This is required when starting a new goroutine and
// passing an existing task context to it.
//
// taskName should be a constantand must not contain '#'
//
// The implementation ensures that,
// if multiple tasks with the same name exist simultaneously,
// a unique suffix is appended to uniquely identify the task opened with this function.
func WithTask(ctx context.Context, taskName string) (context.Context, DoneFunc) {

	var parentTask *traceNode
	nodeI := ctx.Value(contextKeyTraceNode)
	if nodeI != nil {
		node := nodeI.(*traceNode)
		if node.parentSpan != nil {
			parentTask = node.parentSpan // FIXME review this
		} else {
			parentTask = node
		}
	}

	taskName, taskNameDone := taskNamer.UniqueConcurrentTaskName(taskName)

	this := &traceNode{
		id:                 newTraceNodeId(),
		annotation:         taskName,
		parentTask:         parentTask,
		activeChildTasks:   0,
		hasActiveChildSpan: 0,
		parentSpan:         nil,

		startedAt: time.Now(),
		ended:     0,
		endedAt:   time.Time{},
	}

	if this.parentTask != nil {
		atomic.AddInt32(&this.parentTask.activeChildTasks, 1)
	}

	ctx = context.WithValue(ctx, contextKeyTraceNode, this)

	chrometraceBeginTask(this)

	metrics.activeTasks.Inc()

	endTaskFunc := func() {
		if nc := atomic.LoadInt32(&this.activeChildTasks); nc != 0 {
			panic(fmt.Sprintf("this task must have 0 active child tasks, got %v", nc))
		}

		if !atomic.CompareAndSwapInt32(&this.ended, 0, 1) {
			return
		}
		this.endedAt = time.Now()

		if this.parentTask != nil {
			if atomic.AddInt32(&this.parentTask.activeChildTasks, -1) < 0 {
				panic("parent task with negative activeChildTasks count")
			}
		}

		chrometraceEndTask(this)

		metrics.activeTasks.Dec()

		taskNameDone()
	}

	return ctx, endTaskFunc
}

// ctx must have an active task (see WithTask)
//
// spans must nest (stack-like), otherwise, this function panics
func WithSpan(ctx context.Context, annotation string) (context.Context, DoneFunc) {
	var parentSpan, parentTask *traceNode
	nodeI := ctx.Value(contextKeyTraceNode)
	if nodeI != nil {
		parentSpan = nodeI.(*traceNode)
		if parentSpan.parentSpan == nil {
			parentTask = parentSpan
		} else {
			parentTask = parentSpan.parentTask
		}
	} else {
		panic("must be called from within a task")
	}

	this := &traceNode{
		id:                 newTraceNodeId(),
		annotation:         annotation,
		parentTask:         parentTask,
		parentSpan:         parentSpan,
		hasActiveChildSpan: 0,

		startedAt: time.Now(),
		ended:     0,
		endedAt:   time.Time{},
	}

	if !atomic.CompareAndSwapInt32(&parentSpan.hasActiveChildSpan, 0, 1) {
		panic("already has active child span")
	}

	ctx = context.WithValue(ctx, contextKeyTraceNode, this)
	chrometraceBeginSpan(this)

	endTaskFunc := func() {
		if !atomic.CompareAndSwapInt32(&parentSpan.hasActiveChildSpan, 1, 0) {
			panic("impl error: hasActiveChildSpan should not change to 0 while we hold it")
		}
		this.endedAt = time.Now()

		chrometraceEndSpan(this)
	}

	return ctx, endTaskFunc
}

type traceNode struct {
	id                 string
	annotation         string
	parentTask         *traceNode
	activeChildTasks   int32 // only for task nodes, insignificant for span nodes
	parentSpan         *traceNode
	hasActiveChildSpan int32

	startedAt time.Time
	endedAt   time.Time
	ended     int32
}

func currentTaskNameAndSpanStack(this *traceNode) (taskName string, spanIdStack string) {

	task := this.parentTask
	if this.parentSpan == nil {
		task = this
	}

	var spansInTask []*traceNode
	for s := this; s != nil; s = s.parentSpan {
		spansInTask = append(spansInTask, s)
	}

	var tasks []*traceNode
	for t := task; t != nil; t = t.parentTask {
		tasks = append(tasks, t)
	}

	var taskIdsRev []string
	for i := len(tasks) - 1; i >= 0; i-- {
		taskIdsRev = append(taskIdsRev, tasks[i].id)
	}

	var spanIdsRev []string
	for i := len(spansInTask) - 1; i >= 0; i-- {
		spanIdsRev = append(spanIdsRev, spansInTask[i].id)
	}

	taskStack := strings.Join(taskIdsRev, "$")
	spanIdStack = fmt.Sprintf("%s$%s", taskStack, strings.Join(spanIdsRev, "."))

	return task.annotation, spanIdStack
}

var traceNodeIdPRNG = rand.New(rand.NewSource(1))

func init() {
	traceNodeIdPRNG.Seed(time.Now().UnixNano())
	traceNodeIdPRNG.Seed(int64(os.Getpid()))
}

var traceNodeIdBytes = envconst.Int("ZREPL_LOGGING_TRACE_ID_BYTES", 3)

func init() {
	if traceNodeIdBytes < 1 {
		panic("trace node id byte length must be at least 1")
	}
}

func newTraceNodeId() string {
	var out strings.Builder
	enc := base64.NewEncoder(base64.RawStdEncoding, &out)
	buf := make([]byte, traceNodeIdBytes)
	for i := 0; i < len(buf); {
		n, err := traceNodeIdPRNG.Read(buf[i:])
		if err != nil {
			panic(err)
		}
		i += n
	}
	n, err := enc.Write(buf[:])
	if err != nil || n != len(buf) {
		panic(err)
	}
	if err := enc.Close(); err != nil {
		panic(err)
	}
	return out.String()
}
