package driver

import (
	"container/heap"
	"fmt"
	"sync"
	"time"

	"github.com/zrepl/zrepl/util/chainlock"
)

type stepQueueRec struct {
	ident      interface{}
	targetDate time.Time
	wakeup     chan StepCompletedFunc
	cancelDueToConcurrencyDownsize interace{}
}

type stepQueue struct {
	stop chan struct{}
	reqs chan stepQueueRec

	// l protects all members except the channels above

	l           *chainlock.L
	pendingCond *sync.Cond

	// ident => queueItem
	pending    *stepQueueHeap
	active     *stepQueueHeap
	queueItems map[interface{}]*stepQueueHeapItem // for tracking used idents in both pending and active

	// stopped is used for cancellation of "wake" goroutine
	stopped bool

	concurrency int
}

type stepQueueHeapItem struct {
	idx int
	req *stepQueueRec
}
type stepQueueHeap struct {
	items   []*stepQueueHeapItem
	reverse bool // never change after pushing first element
}

func (h stepQueueHeap) Less(i, j int) bool {
	res := h.items[i].req.targetDate.Before(h.items[j].req.targetDate)
	if h.reverse {
		return !res
	}
	return res
}

func (h stepQueueHeap) Swap(i, j int) {
	h.items[i], h.items[j] = h.items[j], h.items[i]
	h.items[i].idx = i
	h.items[j].idx = j
}

func (h stepQueueHeap) Len() int {
	return len(h.items)
}

func (h *stepQueueHeap) Push(elem interface{}) {
	hitem := elem.(*stepQueueHeapItem)
	hitem.idx = h.Len()
	h.items = append(h.items, hitem)
}

func (h *stepQueueHeap) Pop() interface{} {
	elem := h.items[h.Len()-1]
	elem.idx = -1
	h.items = h.items[:h.Len()-1]
	return elem
}

// returned stepQueue must be closed with method Close
func newStepQueue(concurrency int) *stepQueue {
	l := chainlock.New()
	q := &stepQueue{
		stop:        make(chan struct{}),
		reqs:        make(chan stepQueueRec),
		l:           l,
		pendingCond: l.NewCond(),
		// priority queue
		pending: &stepQueueHeap{reverse: false},
		active:  &stepQueueHeap{reverse: true},
		// ident => queueItem
		queueItems: make(map[interface{}]*stepQueueHeapItem),
		// stopped is used for cancellation of "wake" goroutine
		stopped: false,
	}
	err := q.setConcurrencyLocked(concurrency)
	if err != nil {
		panic(err)
	}
	return q
}

// the returned done function must be called to free resources
// allocated by the call to Start
//
// No WaitReady calls must be active at the time done is called
// The behavior of calling WaitReady after done was called is undefined
func (q *stepQueue) Start() (done func()) {
	go func() { // "stopper" goroutine
		<-q.stop
		defer q.l.Lock().Unlock()
		q.stopped = true
		q.pendingCond.Broadcast()
	}()
	go func() { // "reqs" goroutine
		for {
			select {
			case <-q.stop:
				select {
				case <-q.reqs:
					panic("WaitReady call active while calling Close")
				default:
					return
				}
			case req := <-q.reqs:
				func() {
					defer q.l.Lock().Unlock()
					if _, ok := q.queueItems[req.ident]; ok {
						panic("WaitReady must not be called twice for the same ident")
					}
					qitem := &stepQueueHeapItem{
						req: req,
					}
					q.queueItems[req.ident] = qitem
					heap.Push(q.pending, qitem)
					q.pendingCond.Broadcast()
				}()
			}
		}
	}()
	go func() { // "wake" goroutine
		defer q.l.Lock().Unlock()
		for {

			for !q.stopped && (q.active.Len() >= q.concurrency || q.pending.Len() == 0) {
				q.pendingCond.Wait()
			}
			if q.stopped {
				return
			}
			if q.pending.Len() <= 0 {
				return
			}

			// pop from tracked items
			next := heap.Pop(q.pending).(*stepQueueHeapItem)

			next.req.cancelDueToConcurrencyDownsize = 

			heap.Push(q.active, next)

			next.req.wakeup <- func() {
				defer q.l.Lock().Unlock()

				//
				qitem := &stepQueueHeapItem{
					req: req,
				}

				// delete(q.queueItems, next.req.ident) // def

				q.pendingCond.Broadcast()
			}
		}
	}()

	done = func() {
		close(q.stop)
	}
	return done
}

type StepCompletedFunc func()

func (q *stepQueue) sendAndWaitForWakeup(ident interface{}, targetDate time.Time) StepCompletedFunc {
	req := stepQueueRec{
		ident,
		targetDate,
		make(chan StepCompletedFunc),
	}
	q.reqs <- req
	return <-req.wakeup
}

// Wait for the ident with targetDate to be selected to run.
func (q *stepQueue) WaitReady(ident interface{}, targetDate time.Time) StepCompletedFunc {
	if targetDate.IsZero() {
		panic("targetDate of zero is reserved for marking Done")
	}
	return q.sendAndWaitForWakeup(ident, targetDate)
}

// caller must hold lock
func (q *stepQueue) setConcurrencyLocked(newConcurrency int) error {
	if !(newConcurrency >= 1) {
		return fmt.Errorf("concurrency must be >= 1 but requested %v", newConcurrency)
	}
	q.concurrency = newConcurrency
	q.pendingCond.Broadcast() // wake up waiters who could make progress

	for q.active.Len() > q.concurrency {
		item := heap.Pop(q.active).(*stepQueueHeapItem)
		item.req.cancelDueToConcurrencyDownsize()
		heap.Push(q.pending, item)
	}
	return nil
}

func (q *stepQueue) SetConcurrency(new int) error {
	defer q.l.Lock().Unlock()
	return q.setConcurrencyLocked(new)
}
