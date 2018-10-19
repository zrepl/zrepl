package watchdog

import (
	"fmt"
	"sync"
	"time"
)

type Progress struct {
	lastUpd time.Time
}

func (p *Progress) String() string {
	return fmt.Sprintf("last update at %s", p.lastUpd)
}

func (p *Progress) madeProgressSince(p2 *Progress) bool {
	if p.lastUpd.IsZero() && p2.lastUpd.IsZero() {
		return false
	}
	return p.lastUpd.After(p2.lastUpd)
}

type KeepAlive struct {
	mtx sync.Mutex
	p Progress
}

func (k *KeepAlive) MadeProgress() {
	k.mtx.Lock()
	defer k.mtx.Unlock()
	k.p.lastUpd = time.Now()
}

func (k *KeepAlive) ExpectProgress(last *Progress) (madeProgress bool) {
	k.mtx.Lock()
	defer k.mtx.Unlock()
	madeProgress = k.p.madeProgressSince(last)
	*last = k.p
	return madeProgress
}
