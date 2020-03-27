package viewmodel

import "time"

type byteProgressMeasurement struct {
	time time.Time
	val  int64
}

type bytesProgressHistory struct {
	last        *byteProgressMeasurement // pointer as poor man's optional
	changeCount int
	lastChange  time.Time
	bpsAvg      float64
}

func (p *bytesProgressHistory) Update(currentVal int64) (bytesPerSecondAvg int64, changeCount int) {

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

	deltaV := currentVal - p.last.val
	deltaT := time.Since(p.last.time)
	rate := float64(deltaV) / deltaT.Seconds()

	factor := 0.3
	p.bpsAvg = (1-factor)*p.bpsAvg + factor*rate

	p.last.time = time.Now()
	p.last.val = currentVal

	return int64(p.bpsAvg), p.changeCount
}
