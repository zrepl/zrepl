package driver

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/montanaflynn/stats"
	"github.com/stretchr/testify/assert"

	"github.com/LyingCak3/zrepl/internal/daemon/logging/trace"
	"github.com/LyingCak3/zrepl/internal/util/zreplcircleci"
)

func TestPqNotconcurrent(t *testing.T) {
	zreplcircleci.SkipOnCircleCI(t, "because it relies on scheduler responsiveness < 500ms")

	ctx, end := trace.WithTaskFromStack(context.Background())
	defer end()
	var ctr uint32
	q := newStepQueue()
	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		ctx, end := trace.WithTaskFromStack(ctx)
		defer end()
		defer wg.Done()
		defer q.WaitReady(ctx, "1", time.Unix(9999, 0))()
		ret := atomic.AddUint32(&ctr, 1)
		assert.Equal(t, uint32(1), ret)
		time.Sleep(1 * time.Second)
	}()

	// give goroutine "1" 500ms to enter queue, get the active slot and enter time.Sleep
	defer q.Start(1)()
	time.Sleep(500 * time.Millisecond)

	// while "1" is still running, queue in "2", "3" and "4"
	go func() {
		ctx, end := trace.WithTaskFromStack(ctx)
		defer end()
		defer wg.Done()
		defer q.WaitReady(ctx, "2", time.Unix(2, 0))()
		ret := atomic.AddUint32(&ctr, 1)
		assert.Equal(t, uint32(2), ret)
	}()
	go func() {
		ctx, end := trace.WithTaskFromStack(ctx)
		defer end()
		defer wg.Done()
		defer q.WaitReady(ctx, "3", time.Unix(3, 0))()
		ret := atomic.AddUint32(&ctr, 1)
		assert.Equal(t, uint32(3), ret)
	}()
	go func() {
		ctx, end := trace.WithTaskFromStack(ctx)
		defer end()
		defer wg.Done()
		defer q.WaitReady(ctx, "4", time.Unix(4, 0))()
		ret := atomic.AddUint32(&ctr, 1)
		assert.Equal(t, uint32(4), ret)
	}()

	wg.Wait()
}

type record struct {
	fs        int
	step      int
	globalCtr uint32
	wakeAt    time.Duration // relative to begin
}

func (r record) String() string {
	return fmt.Sprintf("fs %08d step %08d globalCtr %08d wakeAt %2.8f", r.fs, r.step, r.globalCtr, r.wakeAt.Seconds())
}

// This tests uses stepPq concurrently, simulating the following scenario:
// Given a number of filesystems F, each filesystem has N steps to take.
// The number of concurrent steps is limited to C.
// The target date for each step is the step number N.
// Hence, there are always F filesystems runnable (calling WaitReady)
// The priority queue prioritizes steps with lower target data (= lower step number).
// Hence, all steps with lower numbers should be woken up before steps with higher numbers.
// However, scheduling is not 100% deterministic (runtime, OS scheduler, etc).
// Hence, perform some statistics on the wakeup times and assert that the mean wakeup
// times for each step are close together.
func TestPqConcurrent(t *testing.T) {
	zreplcircleci.SkipOnCircleCI(t, "because it relies on scheduler responsiveness < 500ms")

	ctx, end := trace.WithTaskFromStack(context.Background())
	defer end()

	q := newStepQueue()
	var wg sync.WaitGroup
	filesystems := 100
	stepsPerFS := 20
	sleepTimePerStep := 50 * time.Millisecond
	wg.Add(filesystems)
	var globalCtr uint32

	begin := time.Now()
	records := make(chan []record, filesystems)
	for fs := 0; fs < filesystems; fs++ {
		go func(fs int) {
			ctx, end := trace.WithTaskFromStack(ctx)
			defer end()
			defer wg.Done()
			recs := make([]record, 0)
			for step := 0; step < stepsPerFS; step++ {
				pos := atomic.AddUint32(&globalCtr, 1)
				t := time.Unix(int64(step), 0)
				done := q.WaitReady(ctx, fs, t)
				wakeAt := time.Since(begin)
				time.Sleep(sleepTimePerStep)
				done()
				recs = append(recs, record{fs, step, pos, wakeAt})
			}
			records <- recs
		}(fs)
	}
	concurrency := 5
	defer q.Start(concurrency)()
	wg.Wait()
	close(records)
	t.Logf("loop done")

	flattenedRecs := make([]record, 0)
	for recs := range records {
		flattenedRecs = append(flattenedRecs, recs...)
	}

	sort.Slice(flattenedRecs, func(i, j int) bool {
		return flattenedRecs[i].globalCtr < flattenedRecs[j].globalCtr
	})

	wakeTimesByStep := map[int][]float64{}
	for _, rec := range flattenedRecs {
		wakeTimes, ok := wakeTimesByStep[rec.step]
		if !ok {
			wakeTimes = []float64{}
		}
		wakeTimes = append(wakeTimes, rec.wakeAt.Seconds())
		wakeTimesByStep[rec.step] = wakeTimes
	}

	meansByStepId := make([]float64, stepsPerFS)
	interQuartileRangesByStepIdx := make([]float64, stepsPerFS)
	for step := 0; step < stepsPerFS; step++ {
		t.Logf("step %d", step)
		mean, _ := stats.Mean(wakeTimesByStep[step])
		meansByStepId[step] = mean
		t.Logf("\tmean: %v", mean)
		median, _ := stats.Median(wakeTimesByStep[step])
		t.Logf("\tmedian: %v", median)
		midhinge, _ := stats.Midhinge(wakeTimesByStep[step])
		t.Logf("\tmidhinge: %v", midhinge)
		min, _ := stats.Min(wakeTimesByStep[step])
		t.Logf("\tmin: %v", min)
		max, _ := stats.Max(wakeTimesByStep[step])
		t.Logf("\tmax: %v", max)
		quartiles, _ := stats.Quartile(wakeTimesByStep[step])
		t.Logf("\t%#v", quartiles)
		interQuartileRange, _ := stats.InterQuartileRange(wakeTimesByStep[step])
		t.Logf("\tinter-quartile range: %v", interQuartileRange)
		interQuartileRangesByStepIdx[step] = interQuartileRange
	}

	iqrMean, _ := stats.Mean(interQuartileRangesByStepIdx)
	t.Logf("inter-quartile-range mean: %v", iqrMean)
	iqrDev, _ := stats.StandardDeviation(interQuartileRangesByStepIdx)
	t.Logf("inter-quartile-range deviation: %v", iqrDev)

	// each step should have the same "distribution" (=~ "spread")
	assert.True(t, iqrDev < 0.01)

	minTimeForAllStepsWithIdxI := sleepTimePerStep.Seconds() * float64(filesystems) / float64(concurrency)
	t.Logf("minTimeForAllStepsWithIdxI = %11.8f", minTimeForAllStepsWithIdxI)
	for i, mean := range meansByStepId {
		// we can't just do (i + 0.5) * minTimeforAllStepsWithIdxI
		// because this doesn't account for drift
		idealMean := 0.5 * minTimeForAllStepsWithIdxI
		if i > 0 {
			previousMean := meansByStepId[i-1]
			idealMean = previousMean + minTimeForAllStepsWithIdxI
		}
		deltaFromIdeal := idealMean - mean
		t.Logf("step %02d delta from ideal mean wake time: %11.8f - %11.8f = %11.8f", i, idealMean, mean, deltaFromIdeal)
		assert.True(t, math.Abs(deltaFromIdeal) < 0.05)
	}

}
