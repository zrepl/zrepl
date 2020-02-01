package driver

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/replication/report"

	"github.com/stretchr/testify/assert"

	jsondiff "github.com/yudai/gojsondiff"
	jsondiffformatter "github.com/yudai/gojsondiff/formatter"
)

type mockPlanner struct {
	stepCounter uint32
	fss         []FS // *mockFS
}

func (p *mockPlanner) Plan(ctx context.Context) ([]FS, error) {
	time.Sleep(1 * time.Second)
	p.fss = []FS{
		&mockFS{
			&p.stepCounter,
			"zroot/one",
			nil,
		},
		&mockFS{
			&p.stepCounter,
			"zroot/two",
			nil,
		},
	}
	return p.fss, nil
}

func (p *mockPlanner) WaitForConnectivity(context.Context) error {
	return nil
}

type mockFS struct {
	globalStepCounter *uint32
	name              string
	steps             []Step
}

func (f *mockFS) EqualToPreviousAttempt(other FS) bool {
	return f.name == other.(*mockFS).name
}

func (f *mockFS) PlanFS(ctx context.Context) ([]Step, error) {
	if f.steps != nil {
		panic("PlanFS used twice")
	}
	switch f.name {
	case "zroot/one":
		f.steps = []Step{
			&mockStep{
				fs:         f,
				ident:      "a",
				duration:   1 * time.Second,
				targetDate: time.Unix(2, 0),
			},
			&mockStep{
				fs:         f,
				ident:      "b",
				duration:   1 * time.Second,
				targetDate: time.Unix(10, 0),
			},
			&mockStep{
				fs:         f,
				ident:      "c",
				duration:   1 * time.Second,
				targetDate: time.Unix(20, 0),
			},
		}
	case "zroot/two":
		f.steps = []Step{
			&mockStep{
				fs:         f,
				ident:      "u",
				duration:   500 * time.Millisecond,
				targetDate: time.Unix(15, 0),
			},
			&mockStep{
				fs:         f,
				duration:   500 * time.Millisecond,
				ident:      "v",
				targetDate: time.Unix(30, 0),
			},
		}
	default:
		panic("unimplemented")
	}

	return f.steps, nil
}

func (f *mockFS) ReportInfo() *report.FilesystemInfo {
	return &report.FilesystemInfo{Name: f.name}
}

type mockStep struct {
	fs         *mockFS
	ident      string
	duration   time.Duration
	targetDate time.Time

	// filled by method Step
	globalCtr uint32
}

func (f *mockStep) String() string {
	return fmt.Sprintf("%s{%s} targetDate=%s globalCtr=%v", f.fs.name, f.ident, f.targetDate, f.globalCtr)
}

func (f *mockStep) Step(ctx context.Context) error {
	f.globalCtr = atomic.AddUint32(f.fs.globalStepCounter, 1)
	time.Sleep(f.duration)
	return nil
}

func (f *mockStep) TargetEquals(s Step) bool {
	return f.ident == s.(*mockStep).ident
}

func (f *mockStep) TargetDate() time.Time {
	return f.targetDate
}

func (f *mockStep) ReportInfo() *report.StepInfo {
	return &report.StepInfo{From: f.ident, To: f.ident, BytesExpected: 100, BytesReplicated: 25}
}

// TODO: add meaningful validation (i.e. actual checks)
// Since the stepqueue is not deterministic due to scheduler jitter,
// we cannot test for any definitive sequence of steps here.
// Such checks would further only be sensible for a non-concurrent step-queue,
// but we're going to have concurrent replication in the future.
//
// For the time being, let's just exercise the code a bit.
func TestReplication(t *testing.T) {

	ctx := context.Background()

	mp := &mockPlanner{}
	getReport, wait, _ := Do(ctx, 1, mp)
	begin := time.Now()
	fireAt := []time.Duration{
		// the following values are relative to the start
		500 * time.Millisecond,  // planning
		1500 * time.Millisecond, // nothing is done, a is running
		2500 * time.Millisecond, // a done,  b running
		3250 * time.Millisecond, // a,b done, u running
		3750 * time.Millisecond, // a,b,u done, c running
		4750 * time.Millisecond, // a,b,u,c done, v running
		5250 * time.Millisecond, // a,b,u,c,v done
	}
	reports := make([]*report.Report, len(fireAt))
	for i := range fireAt {
		sleepUntil := begin.Add(fireAt[i])
		time.Sleep(time.Until(sleepUntil))
		reports[i] = getReport()
		// uncomment for viewing non-diffed results
		// t.Logf("report @ %6.4f:\n%s", fireAt[i].Seconds(), pretty.Sprint(reports[i]))
	}
	waitBegin := time.Now()
	wait(true)
	waitDuration := time.Since(waitBegin)
	assert.True(t, waitDuration < 10*time.Millisecond, "%v", waitDuration) // and that's gratious

	prev, err := json.Marshal(reports[0])
	require.NoError(t, err)
	for _, r := range reports[1:] {
		this, err := json.Marshal(r)
		require.NoError(t, err)
		differ := jsondiff.New()
		diff, err := differ.Compare(prev, this)
		require.NoError(t, err)
		df := jsondiffformatter.NewDeltaFormatter()
		_, err = df.Format(diff)
		require.NoError(t, err)
		// uncomment the following line to get json diffs between each captured step
		// t.Logf("%s", res)
		prev, err = json.Marshal(r)
		require.NoError(t, err)
	}

	steps := make([]*mockStep, 0)
	for _, fs := range mp.fss {
		for _, step := range fs.(*mockFS).steps {
			steps = append(steps, step.(*mockStep))
		}
	}

	// sort steps in pq order (although, remember, pq is not deterministic)
	sort.Slice(steps, func(i, j int) bool {
		return steps[i].targetDate.Before(steps[j].targetDate)
	})

	// manual inspection of the globalCtr value should show that, despite
	// scheduler-dependent behavior of pq, steps should generally be taken
	// from oldest to newest target date (globally, not per FS).
	t.Logf("steps sorted by target date:")
	for _, step := range steps {
		t.Logf("\t%s", step)
	}

}
