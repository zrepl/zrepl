package queue

import (
	"sort"
	"time"

	. "github.com/zrepl/zrepl/replication/fsrep"
)

type replicationQueueItem struct {
	// duplicates fsr.state to avoid accessing and locking fsr
	state State
	// duplicates fsr.current.nextStepDate to avoid accessing & locking fsr
	nextStepDate time.Time
	// duplicates fsr.retryWaitUntil to avoid accessing & locking fsr
	retryWaitUntil time.Time

	fsr *Replication
}

type ReplicationQueue []*replicationQueueItem

func NewReplicationQueue() *ReplicationQueue {
	q := make(ReplicationQueue, 0)
	return &q
}

func (q ReplicationQueue) Len() int      { return len(q) }
func (q ReplicationQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }

type lessmapEntry struct {
	prio int
	less func(a, b *replicationQueueItem) bool
}

var lessmap = map[State]lessmapEntry{
	Ready: {
		prio: 0,
		less: func(a, b *replicationQueueItem) bool {
			return a.nextStepDate.Before(b.nextStepDate)
		},
	},
	RetryWait: {
		prio: 1,
		less: func(a, b *replicationQueueItem) bool {
			return a.retryWaitUntil.Before(b.retryWaitUntil)
		},
	},
}

func (q ReplicationQueue) Less(i, j int) bool {

	a, b := q[i], q[j]
	al, aok := lessmap[a.state]
	if !aok {
		panic(a)
	}
	bl, bok := lessmap[b.state]
	if !bok {
		panic(b)
	}

	if al.prio != bl.prio {
		return al.prio < bl.prio
	}

	return al.less(a, b)
}

func (q *ReplicationQueue) sort() (done []*Replication) {
	// pre-scan for everything that is not ready
	newq := make(ReplicationQueue, 0, len(*q))
	done = make([]*Replication, 0, len(*q))
	for _, qitem := range *q {
		if _, ok := lessmap[qitem.state]; !ok {
			done = append(done, qitem.fsr)
		} else {
			newq = append(newq, qitem)
		}
	}
	sort.Stable(newq) // stable to avoid flickering in reports
	*q = newq
	return done
}

// next remains valid until the next call to GetNext()
func (q *ReplicationQueue) GetNext() (done []*Replication, next *ReplicationQueueItemHandle) {
	done = q.sort()
	if len(*q) == 0 {
		return done, nil
	}
	next = &ReplicationQueueItemHandle{(*q)[0]}
	return done, next
}

func (q *ReplicationQueue) Add(fsr *Replication) {
	*q = append(*q, &replicationQueueItem{
		fsr:   fsr,
		state: fsr.State(),
	})
}

func (q *ReplicationQueue) Foreach(fu func(*ReplicationQueueItemHandle)) {
	for _, qitem := range *q {
		fu(&ReplicationQueueItemHandle{qitem})
	}
}

type ReplicationQueueItemHandle struct {
	i *replicationQueueItem
}

func (h ReplicationQueueItemHandle) GetFSReplication() *Replication {
	return h.i.fsr
}

func (h ReplicationQueueItemHandle) Update(newState State, nextStepDate, retryWaitUntil time.Time) {
	h.i.state = newState
	h.i.nextStepDate = nextStepDate
	h.i.retryWaitUntil = retryWaitUntil
}
