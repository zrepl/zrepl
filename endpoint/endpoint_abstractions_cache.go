package endpoint

import (
	"context"
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/zrepl/zrepl/daemon/logging/trace"
	"github.com/zrepl/zrepl/util/chainlock"
)

var abstractionsCacheMetrics struct {
	count prometheus.Gauge
}

func init() {
	abstractionsCacheMetrics.count = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "zrepl",
		Subsystem: "endpoint",
		Name:      "abstractions_cache_entry_count",
		Help:      "number of abstractions tracked in the abstractionsCache data structure",
	})
}

var abstractionsCacheSingleton = newAbstractionsCache()

func AbstractionsCacheInvalidate(fs string) {
	abstractionsCacheSingleton.InvalidateFSCache(fs)
}

type abstractionsCacheDidLoadFSState int

const (
	abstractionsCacheDidLoadFSStateNo abstractionsCacheDidLoadFSState = iota // 0-value has meaning
	abstractionsCacheDidLoadFSStateInProgress
	abstractionsCacheDidLoadFSStateDone
)

type abstractionsCache struct {
	mtx              chainlock.L
	abstractions     []Abstraction
	didLoadFS        map[string]abstractionsCacheDidLoadFSState
	didLoadFSChanged *sync.Cond
}

func newAbstractionsCache() *abstractionsCache {
	c := &abstractionsCache{
		didLoadFS: make(map[string]abstractionsCacheDidLoadFSState),
	}
	c.didLoadFSChanged = c.mtx.NewCond()
	return c
}

func (s *abstractionsCache) Put(a Abstraction) {
	defer s.mtx.Lock().Unlock()

	var zeroJobId JobID
	if a.GetJobID() == nil {
		panic("abstraction must not have nil job id")
	} else if *a.GetJobID() == zeroJobId {
		panic(fmt.Sprintf("abstraction must not have zero-value job id: %s", a))
	}

	s.abstractions = append(s.abstractions, a)
	abstractionsCacheMetrics.count.Set(float64(len(s.abstractions)))
}

func (s *abstractionsCache) InvalidateFSCache(fs string) {
	// FIXME: O(n)
	newAbs := make([]Abstraction, 0, len(s.abstractions))
	for _, a := range s.abstractions {
		if a.GetFS() != fs {
			newAbs = append(newAbs, a)
		}
	}
	s.abstractions = newAbs
	abstractionsCacheMetrics.count.Set(float64(len(s.abstractions)))

	s.didLoadFS[fs] = abstractionsCacheDidLoadFSStateNo
	s.didLoadFSChanged.Broadcast()

}

// - logs errors in getting on-disk abstractions
// - only fetches on-disk abstractions once, but every time from the in-memory store
//
// That means that for precise results, all abstractions created by the endpoint must be .Put into this cache.
func (s *abstractionsCache) GetAndDeleteByJobIDAndFS(ctx context.Context, jobID JobID, fs string, types AbstractionTypeSet, keep func(a Abstraction) bool) (ret []Abstraction) {
	defer s.mtx.Lock().Unlock()
	defer trace.WithSpanFromStackUpdateCtx(&ctx)()
	var zeroJobId JobID
	if jobID == zeroJobId {
		panic("must not pass zero-value job id")
	}
	if fs == "" {
		panic("must not pass zero-value fs")
	}

	s.tryLoadOnDiskAbstractions(ctx, fs)

	// FIXME O(n)
	var remaining []Abstraction
	for _, a := range s.abstractions {
		aJobId := *a.GetJobID()
		aFS := a.GetFS()
		if aJobId == jobID && aFS == fs && types[a.GetType()] && !keep(a) {
			ret = append(ret, a)
		} else {
			remaining = append(remaining, a)
		}
	}
	s.abstractions = remaining
	abstractionsCacheMetrics.count.Set(float64(len(s.abstractions)))

	return ret
}

// caller must hold s.mtx
func (s *abstractionsCache) tryLoadOnDiskAbstractions(ctx context.Context, fs string) {
	for s.didLoadFS[fs] != abstractionsCacheDidLoadFSStateDone {
		if s.didLoadFS[fs] == abstractionsCacheDidLoadFSStateInProgress {
			s.didLoadFSChanged.Wait()
			continue
		}
		if s.didLoadFS[fs] != abstractionsCacheDidLoadFSStateNo {
			panic(fmt.Sprintf("unreachable: %v", s.didLoadFS[fs]))
		}

		s.didLoadFS[fs] = abstractionsCacheDidLoadFSStateInProgress
		defer s.didLoadFSChanged.Broadcast()

		var onDiskAbs []Abstraction
		var err error
		s.mtx.DropWhile(func() {
			onDiskAbs, err = s.tryLoadOnDiskAbstractionsImpl(ctx, fs) // no shadow
		})

		if err != nil {
			s.didLoadFS[fs] = abstractionsCacheDidLoadFSStateNo
			getLogger(ctx).WithField("fs", fs).WithError(err).Error("cannot list abstractions for filesystem")
		} else {
			s.didLoadFS[fs] = abstractionsCacheDidLoadFSStateDone
			s.abstractions = append(s.abstractions, onDiskAbs...)
			getLogger(ctx).WithField("fs", fs).WithField("abstractions", onDiskAbs).Debug("loaded step abstractions for filesystem")
		}
		return
	}
}

// caller should _not hold s.mtx
func (s *abstractionsCache) tryLoadOnDiskAbstractionsImpl(ctx context.Context, fs string) ([]Abstraction, error) {
	defer trace.WithSpanFromStackUpdateCtx(&ctx)()

	q := ListZFSHoldsAndBookmarksQuery{
		FS: ListZFSHoldsAndBookmarksQueryFilesystemFilter{
			FS: &fs,
		},
		JobID: nil,
		What: AbstractionTypeSet{
			AbstractionStepHold:                           true,
			AbstractionTentativeReplicationCursorBookmark: true,
			AbstractionReplicationCursorBookmarkV2:        true,
			AbstractionLastReceivedHold:                   true,
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

func (s *abstractionsCache) TryBatchDestroy(ctx context.Context, jobId JobID, fs string, types AbstractionTypeSet, keep func(a Abstraction) bool, check func(willDestroy []Abstraction)) {
	// no s.mtx, we only use the public interface in this function

	defer trace.WithSpanFromStackUpdateCtx(&ctx)()

	obsoleteAbs := s.GetAndDeleteByJobIDAndFS(ctx, jobId, fs, types, keep)

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
				Error("cannot destroy abstraction")
		} else {
			getLogger(ctx).
				WithField("abstraction", res.Abstraction).
				Info("destroyed abstraction")
		}
	}
	if hadErr {
		s.InvalidateFSCache(fs)
	}

}
