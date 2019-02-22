package driver

import (
	"container/heap"
	"time"

	"github.com/zrepl/zrepl/util/chainlock"
)

type stepQueueRec struct {
	ident      interface{}
	targetDate time.Time
	wakeup     chan StepCompletedFunc
}

type stepQueue struct {
	stop chan struct{}
	reqs chan stepQueueRec
}

type stepQueueHeapItem struct {
	idx int
	req stepQueueRec
}
type stepQueueHeap []*stepQueueHeapItem

func (h stepQueueHeap) Less(i, j int) bool {
	return h[i].req.targetDate.Before(h[j].req.targetDate)
}

func (h stepQueueHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].idx = i
	h[j].idx = j
}

func (h stepQueueHeap) Len() int {
	return len(h)
}

func (h *stepQueueHeap) Push(elem interface{}) {
	hitem := elem.(*stepQueueHeapItem)
	hitem.idx = h.Len()
	*h = append(*h, hitem)
}

func (h *stepQueueHeap) Pop() interface{} {
	elem := (*h)[h.Len()-1]
	elem.idx = -1
	*h = (*h)[:h.Len()-1]
	return elem
}

// returned stepQueue must be closed with method Close
func newStepQueue() *stepQueue {
	q := &stepQueue{
		stop: make(chan struct{}),
		reqs: make(chan stepQueueRec),
	}
	return q
}

// the returned done function must be called to free resources
// allocated by the call to Start
//
// No WaitReady calls must be active at the time done is called
// The behavior of calling WaitReady after done was called is undefined
func (q *stepQueue) Start(concurrency int) (done func()) {
	if concurrency < 1 {
		panic("concurrency must be >= 1")
	}
	// l protects pending and queueItems
	l := chainlock.New()
	pendingCond := l.NewCond()
	// priority queue
	pending := &stepQueueHeap{}
	// ident => queueItem
	queueItems := make(map[interface{}]*stepQueueHeapItem)
	// stopped is used for cancellation of "wake" goroutine
	stopped := false
	active := 0
	go func() { // "stopper" goroutine
		<-q.stop
		defer l.Lock().Unlock()
		stopped = true
		pendingCond.Broadcast()
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
					defer l.Lock().Unlock()
					if _, ok := queueItems[req.ident]; ok {
						panic("WaitReady must not be called twice for the same ident")
					}
					qitem := &stepQueueHeapItem{
						req: req,
					}
					queueItems[req.ident] = qitem
					heap.Push(pending, qitem)
					pendingCond.Broadcast()
				}()
			}
		}
	}()
	go func() { // "wake" goroutine
		defer l.Lock().Unlock()
		for {

			for !stopped && (active >= concurrency || pending.Len() == 0) {
				pendingCond.Wait()
			}
			if stopped {
				return
			}
			if pending.Len() <= 0 {
				return
			}
			active++
			next := heap.Pop(pending).(*stepQueueHeapItem).req
			delete(queueItems, next.ident)

			next.wakeup <- func() {
				defer l.Lock().Unlock()
				active--
				pendingCond.Broadcast()
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
