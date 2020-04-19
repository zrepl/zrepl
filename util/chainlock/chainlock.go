// package chainlock implements a mutex whose Lock and Unlock
// methods return the lock itself, to enable chaining.
//
// Intended Usage
//
//   defer s.lock().unlock()
//   // drop lock while waiting for wait group
//   func() {
//	     defer a.l.Unlock().Lock()
//	     fssesDone.Wait()
//	 }()
//
package chainlock

import "sync"

type L struct {
	mtx sync.Mutex
}

func New() *L {
	return &L{}
}

func (l *L) Lock() *L {
	l.mtx.Lock()
	return l
}

func (l *L) Unlock() *L {
	l.mtx.Unlock()
	return l
}

func (l *L) NewCond() *sync.Cond {
	return sync.NewCond(&l.mtx)
}

func (l *L) DropWhile(f func()) {
	defer l.Unlock().Lock()
	f()
}

func (l *L) HoldWhile(f func()) {
	defer l.Lock().Unlock()
	f()
}
