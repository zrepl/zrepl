package viewmodel

import "time"

type byteProgressMeasurement struct {
	time time.Time
	val  uint64
}

type bytesProgressHistory struct {
	last        *byteProgressMeasurement // pointer as poor man's optional
	changeCount int
	lastChange  time.Time
}

func (p *bytesProgressHistory) Update(currentVal uint64) (bytesPerSecondAvg int64, changeCount int) {

	if p.last == nil {
		p.last = &byteProgressMeasurement{
			time: time.Now(),
			val:  currentVal,
		}
		return 0, 0
	}

	if p.last.val != currentVal {
		p.changeCount++
		p.lastChange = time.Now()
	}

	if time.Since(p.lastChange) > 3*time.Second {
		p.last = nil
		return 0, 0
	}

	var deltaV int64
	if currentVal >= p.last.val {
		deltaV = int64(currentVal - p.last.val)
	} else {
		deltaV = -int64(p.last.val - currentVal)
	}
	deltaT := time.Since(p.last.time)
	rate := float64(deltaV) / deltaT.Seconds()

	p.last.time = time.Now()
	p.last.val = currentVal

	return int64(rate), p.changeCount
}
