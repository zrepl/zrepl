package util

type Semaphore struct {
	c chan struct{}
}

func NewSemaphore(cap int) Semaphore {
	return Semaphore{make(chan struct{}, cap)}
}

func (s Semaphore) Down() {
	s.c <- struct{}{}
}

func (s Semaphore) Up() {
	<-s.c
}
