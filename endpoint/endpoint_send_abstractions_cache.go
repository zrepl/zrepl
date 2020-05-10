package endpoint

import (
	"context"
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/zrepl/zrepl/daemon/logging/trace"
	"github.com/zrepl/zrepl/util/chainlock"
)

var sendAbstractionsCacheMetrics struct {
	count prometheus.Gauge
}

func init() {
	sendAbstractionsCacheMetrics.count = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "zrepl",
		Subsystem: "endpoint",
		Name:      "send_abstractions_cache_entry_count",
		Help:      "number of send abstractions tracked in the sendAbstractionsCache data structure",
	})
}

var sendAbstractionsCacheSingleton = newSendAbstractionsCache()

type sendAbstractionsCacheDidLoadFSState int

const (
	sendAbstractionsCacheDidLoadFSStateNo sendAbstractionsCacheDidLoadFSState = iota // 0-value has meaning
	sendAbstractionsCacheDidLoadFSStateInProgress
	sendAbstractionsCacheDidLoadFSStateDone
)

type sendAbstractionsCache struct {
	mtx              chainlock.L
	abstractions     []Abstraction
	didLoadFS        map[string]sendAbstractionsCacheDidLoadFSState
	didLoadFSChanged *sync.Cond
}

func newSendAbstractionsCache() *sendAbstractionsCache {
	c := &sendAbstractionsCache{
		didLoadFS: make(map[string]sendAbstractionsCacheDidLoadFSState),
	}
	c.didLoadFSChanged = c.mtx.NewCond()
	return c
}

func (s *sendAbstractionsCache) Put(a Abstraction) {
	defer s.mtx.Lock().Unlock()

	var zeroJobId JobID
	if a.GetJobID() == nil {
		panic("abstraction must not have nil job id")
	} else if *a.GetJobID() == zeroJobId {
		panic(fmt.Sprintf("abstraction must not have zero-value job id: %s", a))
	}

	s.abstractions = append(s.abstractions, a)
	sendAbstractionsCacheMetrics.count.Set(float64(len(s.abstractions)))
}

func (s *sendAbstractionsCache) InvalidateFSCache(fs string) {
	// FIXME: O(n)
	newAbs := make([]Abstraction, 0, len(s.abstractions))
	for _, a := range s.abstractions {
		if a.GetFS() != fs {
			newAbs = append(newAbs, a)
		}
	}
	s.abstractions = newAbs
	sendAbstractionsCacheMetrics.count.Set(float64(len(s.abstractions)))

	s.didLoadFS[fs] = sendAbstractionsCacheDidLoadFSStateNo
	s.didLoadFSChanged.Broadcast()

}

// - logs errors in getting on-disk abstractions
// - only fetches on-disk abstractions once, but every time from the in-memory store
//
// That means that for precise results, all abstractions created by the endpoint must be .Put into this cache.
func (s *sendAbstractionsCache) GetAndDeleteByJobIDAndFS(ctx context.Context, jobID JobID, fs string) (ret []Abstraction) {
	defer s.mtx.Lock().Unlock()
	defer trace.WithSpanFromStackUpdateCtx(&ctx)()
	var zeroJobId JobID
	if jobID == zeroJobId {
		panic("must not pass zero-value job id")
	}
	if fs == "" {
		panic("must not pass zero-value fs")
	}

	s.tryLoadOnDiskSendAbstractions(ctx, fs)

	// FIXME O(n)
	var remaining []Abstraction
	for _, a := range s.abstractions {
		aJobId := *a.GetJobID()
		aFS := a.GetFS()
		if aJobId == jobID && aFS == fs {
			ret = append(ret, a)
		} else {
			remaining = append(remaining, a)
		}
	}
	s.abstractions = remaining
	sendAbstractionsCacheMetrics.count.Set(float64(len(s.abstractions)))

	return ret
}

// caller must hold s.mtx
func (s *sendAbstractionsCache) tryLoadOnDiskSendAbstractions(ctx context.Context, fs string) {
	for s.didLoadFS[fs] != sendAbstractionsCacheDidLoadFSStateDone {
		if s.didLoadFS[fs] == sendAbstractionsCacheDidLoadFSStateInProgress {
			s.didLoadFSChanged.Wait()
			continue
		}
		if s.didLoadFS[fs] != sendAbstractionsCacheDidLoadFSStateNo {
			panic(fmt.Sprintf("unreachable: %v", s.didLoadFS[fs]))
		}

		s.didLoadFS[fs] = sendAbstractionsCacheDidLoadFSStateInProgress
		defer s.didLoadFSChanged.Broadcast()

		var onDiskAbs []Abstraction
		var err error
		s.mtx.DropWhile(func() {
			onDiskAbs, err = s.tryLoadOnDiskSendAbstractionsImpl(ctx, fs) // no shadow
		})

		if err != nil {
			s.didLoadFS[fs] = sendAbstractionsCacheDidLoadFSStateNo
			getLogger(ctx).WithField("fs", fs).WithError(err).Error("cannot list send step abstractions for filesystem")
		} else {
			s.didLoadFS[fs] = sendAbstractionsCacheDidLoadFSStateDone
			s.abstractions = append(s.abstractions, onDiskAbs...)
			getLogger(ctx).WithField("fs", fs).WithField("abstractions", onDiskAbs).Debug("loaded step abstractions for filesystem")
		}
		return
	}
}

// caller should _not hold s.mtx
func (s *sendAbstractionsCache) tryLoadOnDiskSendAbstractionsImpl(ctx context.Context, fs string) ([]Abstraction, error) {
	defer trace.WithSpanFromStackUpdateCtx(&ctx)()

	q := ListZFSHoldsAndBookmarksQuery{
		FS: ListZFSHoldsAndBookmarksQueryFilesystemFilter{
			FS: &fs,
		},
		JobID: nil,
		What: AbstractionTypeSet{
			AbstractionStepHold:                    true,
			AbstractionStepBookmark:                true,
			AbstractionReplicationCursorBookmarkV2: true,
		},
		Concurrency: 1,
	}
	abs, absErrs, err := ListAbstractions(ctx, q)
	if err != nil {
		return nil, err
	}
	// safe to ignore absErrs here, this is best-effort cleanup
	if len(absErrs) > 0 {
		return nil, ListAbstractionsErrors(absErrs)
	}
	return abs, nil
}

func (s *sendAbstractionsCache) TryBatchDestroy(ctx context.Context, jobId JobID, fs string, keep func(a Abstraction) bool, check func(willDestroy []Abstraction)) {
	// no s.mtx, we only use the public interface in this function

	defer trace.WithSpanFromStackUpdateCtx(&ctx)()

	allSendStepAbstractions := s.GetAndDeleteByJobIDAndFS(ctx, jobId, fs)

	var obsoleteAbs []Abstraction
	for _, a := range allSendStepAbstractions {
		if !keep(a) {
			obsoleteAbs = append(obsoleteAbs, a)
		}
	}

	if check != nil {
		check(obsoleteAbs)
	}

	hadErr := false
	for res := range BatchDestroy(ctx, obsoleteAbs) {
		if res.DestroyErr != nil {
			hadErr = true
			getLogger(ctx).
				WithField("abstraction", res.Abstraction).
				WithError(res.DestroyErr).
				Error("cannot destroy stale send step abstraction")
		} else {
			getLogger(ctx).
				WithField("abstraction", res.Abstraction).
				Info("destroyed stale send step abstraction")
		}
	}
	if hadErr {
		s.InvalidateFSCache(fs)
	}

}
