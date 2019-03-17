package pruner

import (
	"sort"
	"strings"
	"sync"
)

type execQueue struct {
	mtx sync.Mutex
	pending, completed []*fs
}

func newExecQueue(cap int) *execQueue {
	q := execQueue{
		pending: make([]*fs, 0, cap),
		completed: make([]*fs, 0, cap),
	}
	return &q
}

func (q *execQueue) Report() (pending, completed []FSReport) {
	q.mtx.Lock()
	defer q.mtx.Unlock()

	pending = make([]FSReport, len(q.pending))
	for i, fs := range q.pending {
		pending[i] = fs.Report()
	}
	completed = make([]FSReport, len(q.completed))
	for i, fs := range q.completed {
		completed[i] = fs.Report()
	}

	return pending, completed
}

func (q *execQueue) HasCompletedFSWithErrors() bool {
	q.mtx.Lock()
	defer q.mtx.Unlock()
	for _, fs := range q.completed {
		if fs.execErrLast != nil {
			return true
		}
	}
	return false
}

func (q *execQueue) Pop() *fs {
	if len(q.pending) == 0 {
		return nil
	}
	fs := q.pending[0]
	q.pending = q.pending[1:]
	return fs
}

func(q *execQueue) Put(fs *fs, err error, done bool) {
	fs.mtx.Lock()
	fs.execErrLast = err
	if done || err != nil {
		fs.mtx.Unlock()
		q.mtx.Lock()
		q.completed = append(q.completed, fs)
		q.mtx.Unlock()
		return
	}
	fs.mtx.Unlock()

	q.mtx.Lock()
	// inefficient priority q
	q.pending = append(q.pending, fs)
	sort.SliceStable(q.pending, func(i, j int) bool {
		q.pending[i].mtx.Lock()
		defer q.pending[i].mtx.Unlock()
		q.pending[j].mtx.Lock()
		defer q.pending[j].mtx.Unlock()
		return strings.Compare(q.pending[i].path, q.pending[j].path) == -1
	})
	q.mtx.Unlock()


}