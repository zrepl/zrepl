package watchdog

import (
	"fmt"
	"sync"
	"time"
)

type KeepAlive struct {
	mtx sync.Mutex
	lastUpd time.Time
}

func (p *KeepAlive) String() string {
	if p.lastUpd.IsZero() {
		return fmt.Sprintf("never updated")
	}
	return fmt.Sprintf("last update at %s", p.lastUpd)
}

func (k *KeepAlive) MadeProgress() {
	k.mtx.Lock()
	defer k.mtx.Unlock()
	k.lastUpd = time.Now()
}

func (k *KeepAlive) CheckTimeout(timeout time.Duration, jitter time.Duration) (didTimeOut bool) {
	k.mtx.Lock()
	defer k.mtx.Unlock()
	return k.lastUpd.Add(timeout - jitter).Before(time.Now())
}
