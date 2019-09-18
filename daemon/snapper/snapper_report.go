package snapper

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zrepl/zrepl/daemon/hooks"
)

type Report struct {
	State State
	// valid in state SyncUp and Waiting
	SleepUntil time.Time
	// valid in state Err
	Error string
	// valid in state Snapshotting
	Progress []*ReportFilesystem
}

type ReportFilesystem struct {
	Path  string
	State SnapState

	// Valid in SnapStarted and later
	SnapName      string
	StartAt       time.Time
	Hooks         string
	HooksHadError bool

	// Valid in SnapDone | SnapError
	DoneAt time.Time
}

func errOrEmptyString(e error) string {
	if e != nil {
		return e.Error()
	}
	return ""
}

func (s *Snapper) Report() *Report {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	pReps := make([]*ReportFilesystem, 0, len(s.plan))
	for fs, p := range s.plan {
		var hooksStr string
		var hooksHadError bool
		if p.hookPlan != nil {
			hr := p.hookPlan.Report()
			// FIXME: technically this belongs into client
			// but we can't serialize hooks.Step ATM
			rightPad := func(str string, length int, pad string) string {
				if len(str) > length {
					return str[:length]
				}
				return str + strings.Repeat(pad, length-len(str))
			}
			hooksHadError = hr.HadError()
			rows := make([][]string, len(hr))
			const numCols = 4
			lens := make([]int, numCols)
			for i, e := range hr {
				rows[i] = make([]string, numCols)
				rows[i][0] = fmt.Sprintf("%d", i+1)
				rows[i][1] = e.Status.String()
				runTime := "..."
				if e.Status != hooks.StepPending {
					runTime = e.End.Sub(e.Begin).Round(time.Millisecond).String()
				}
				rows[i][2] = runTime
				rows[i][3] = ""
				if e.Report != nil {
					rows[i][3] = e.Report.String()
				}
				for j, col := range lens {
					if len(rows[i][j]) > col {
						lens[j] = len(rows[i][j])
					}
				}
			}
			rowsFlat := make([]string, len(hr))
			for i, r := range rows {
				colsPadded := make([]string, len(r))
				for j, c := range r[:len(r)-1] {
					colsPadded[j] = rightPad(c, lens[j], " ")
				}
				colsPadded[len(r)-1] = r[len(r)-1]
				rowsFlat[i] = strings.Join(colsPadded, " ")
			}
			hooksStr = strings.Join(rowsFlat, "\n")
		}
		pReps = append(pReps, &ReportFilesystem{
			Path:          fs.ToString(),
			State:         p.state,
			SnapName:      p.name,
			StartAt:       p.startAt,
			DoneAt:        p.doneAt,
			Hooks:         hooksStr,
			HooksHadError: hooksHadError,
		})
	}

	sort.Slice(pReps, func(i, j int) bool {
		return strings.Compare(pReps[i].Path, pReps[j].Path) == -1
	})

	r := &Report{
		State:      s.state,
		SleepUntil: s.sleepUntil,
		Error:      errOrEmptyString(s.err),
		Progress:   pReps,
	}

	return r
}
