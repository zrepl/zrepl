package frameconn

import "sync"

type shutdownFSM struct {
	mtx   sync.Mutex
	state shutdownFSMState
}

type shutdownFSMState uint32

const (
	// zero value is important
	shutdownStateOpen shutdownFSMState = iota
	shutdownStateBegin
)

func (f *shutdownFSM) Begin() (thisCallStartedShutdown bool) {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	thisCallStartedShutdown = f.state != shutdownStateOpen
	f.state = shutdownStateBegin
	return thisCallStartedShutdown
}

func (f *shutdownFSM) IsShuttingDown() bool {
	f.mtx.Lock()
	defer f.mtx.Unlock()
	return f.state != shutdownStateOpen
}
