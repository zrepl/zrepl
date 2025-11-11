package viewmodel

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	yaml "github.com/zrepl/yaml-config"

	"github.com/LyingCak3/zrepl/internal/client/status/viewmodel/stringbuilder"
	"github.com/LyingCak3/zrepl/internal/daemon"
	"github.com/LyingCak3/zrepl/internal/daemon/job"
	"github.com/LyingCak3/zrepl/internal/daemon/pruner"
	"github.com/LyingCak3/zrepl/internal/daemon/snapper"
	"github.com/LyingCak3/zrepl/internal/replication/report"
)

type M struct {
	jobs            map[string]*Job
	jobsList        []*Job
	selectedJob     *Job
	dateString      string
	bottomBarStatus string
}

type Job struct {
	// long-lived
	name         string
	byteProgress *bytesProgressHistory

	lastStatus      *job.Status
	fulldescription string
}

func New() *M {
	return &M{
		jobs:        make(map[string]*Job),
		jobsList:    make([]*Job, 0),
		selectedJob: nil,
	}
}

type FilterFunc func(string) bool

type Params struct {
	Report                  map[string]*job.Status
	ReportFetchError        error
	SelectedJob             *Job
	FSFilter                FilterFunc `validate:"required"`
	DetailViewWidth         int        `validate:"gte=1"`
	DetailViewWrap          bool
	ShortKeybindingOverview string
}

var validate = validator.New()

func (m *M) Update(p Params) {

	if err := validate.Struct(p); err != nil {
		panic(err)
	}

	if p.ReportFetchError != nil {
		m.bottomBarStatus = fmt.Sprintf("[red::]status fetch: %s", p.ReportFetchError)
	} else {
		m.bottomBarStatus = p.ShortKeybindingOverview
		for jobname, st := range p.Report {
			// TODO handle job renames & deletions
			j, ok := m.jobs[jobname]
			if !ok {
				j = &Job{
					name:         jobname,
					byteProgress: &bytesProgressHistory{},
				}
				m.jobs[jobname] = j
				m.jobsList = append(m.jobsList, j)
			}
			j.lastStatus = st
		}
	}

	// filter out internal jobs
	var jobsList []*Job
	for _, j := range m.jobsList {
		if daemon.IsInternalJobName(j.name) {
			continue
		}
		jobsList = append(jobsList, j)
	}
	m.jobsList = jobsList

	// determinism!
	sort.Slice(m.jobsList, func(i, j int) bool {
		return strings.Compare(m.jobsList[i].name, m.jobsList[j].name) < 0
	})

	// try to not lose the selected job
	m.selectedJob = nil
	for _, j := range m.jobsList {
		j.updateFullDescription(p)
		if j == p.SelectedJob {
			m.selectedJob = j
		}
	}

	m.dateString = time.Now().Format(time.RFC3339)

}

func (m *M) BottomBarStatus() string { return m.bottomBarStatus }

func (m *M) Jobs() []*Job { return m.jobsList }

// may be nil
func (m *M) SelectedJob() *Job { return m.selectedJob }

func (m *M) DateString() string { return m.dateString }

func (j *Job) updateFullDescription(p Params) {
	width := p.DetailViewWidth
	if !p.DetailViewWrap {
		width = 10000000 // FIXME
	}
	b := stringbuilder.New(stringbuilder.Config{
		IndentMultiplier: 3,
		Width:            width,
	})
	drawJob(b, j.name, j.lastStatus, j.byteProgress, p.FSFilter)
	j.fulldescription = b.String()
}

func (j *Job) JobTreeTitle() string {
	return j.name
}

func (j *Job) FullDescription() string {
	return j.fulldescription
}

func (j *Job) Name() string {
	return j.name
}

func drawJob(t *stringbuilder.B, name string, v *job.Status, history *bytesProgressHistory, fsfilter FilterFunc) {

	t.Printf("Job: %s\n", name)
	t.Printf("Type: %s\n\n", v.Type)

	if v.Type == job.TypePush || v.Type == job.TypePull {
		activeStatus, ok := v.JobSpecific.(*job.ActiveSideStatus)
		if !ok || activeStatus == nil {
			t.Printf("ActiveSideStatus is null")
			t.Newline()
			return
		}

		t.Printf("Replication:")
		t.AddIndentAndNewline(1)
		renderReplicationReport(t, activeStatus.Replication, history, fsfilter)
		t.AddIndentAndNewline(-1)

		t.Printf("Pruning Sender:")
		t.AddIndentAndNewline(1)
		renderPrunerReport(t, activeStatus.PruningSender, fsfilter)
		t.AddIndentAndNewline(-1)

		t.Printf("Pruning Receiver:")
		t.AddIndentAndNewline(1)
		renderPrunerReport(t, activeStatus.PruningReceiver, fsfilter)
		t.AddIndentAndNewline(-1)

		if v.Type == job.TypePush {
			t.Printf("Snapshotting:")
			t.AddIndentAndNewline(1)
			renderSnapperReport(t, activeStatus.Snapshotting, fsfilter)
			t.AddIndentAndNewline(-1)
		}

	} else if v.Type == job.TypeSnap {
		snapStatus, ok := v.JobSpecific.(*job.SnapJobStatus)
		if !ok || snapStatus == nil {
			t.Printf("SnapJobStatus is null")
			t.Newline()
			return
		}
		t.Printf("Pruning snapshots:")
		t.AddIndentAndNewline(1)
		renderPrunerReport(t, snapStatus.Pruning, fsfilter)
		t.AddIndentAndNewline(-1)
		t.Printf("Snapshotting:")
		t.AddIndentAndNewline(1)
		renderSnapperReport(t, snapStatus.Snapshotting, fsfilter)
		t.AddIndentAndNewline(-1)
	} else if v.Type == job.TypeSource {

		st := v.JobSpecific.(*job.PassiveStatus)
		t.Printf("Snapshotting:\n")
		t.AddIndent(1)
		renderSnapperReport(t, st.Snapper, fsfilter)
		t.AddIndentAndNewline(-1)

	} else {
		t.Printf("No status representation for job type '%s', dumping as YAML", v.Type)
		t.Newline()
		asYaml, err := yaml.Marshal(v.JobSpecific)
		if err != nil {
			t.Printf("Error marshaling status to YAML: %s", err)
			t.Newline()
			return
		}
		t.Write(string(asYaml))
		t.Newline()
	}
}

func printFilesystemStatus(t *stringbuilder.B, rep *report.FilesystemReport, maxFS int) {

	expected, replicated, containsInvalidSizeEstimates := rep.BytesSum()
	sizeEstimationImpreciseNotice := ""
	if containsInvalidSizeEstimates {
		sizeEstimationImpreciseNotice = " (some steps lack size estimation)"
	}
	if rep.CurrentStep < len(rep.Steps) && rep.Steps[rep.CurrentStep].Info.BytesExpected == 0 {
		sizeEstimationImpreciseNotice = " (step lacks size estimation)"
	}

	userVisisbleCurrentStep, userVisibleTotalSteps := rep.CurrentStep, len(rep.Steps)
	// `.CurrentStep` is == len(rep.Steps) if all steps are done.
	// Until then, it's an index into .Steps that starts at 0.
	// For the user, we want it to start at 1.
	if rep.CurrentStep >= len(rep.Steps) {
		// rep.CurrentStep is what we want to show.
		// We check for >= and not == for robustness.
	} else {
		// We're not done yet, so, make step count start at 1
		// (The `.State` is included in the output, indicating we're not done yet)
		userVisisbleCurrentStep = rep.CurrentStep + 1
	}
	status := fmt.Sprintf("%s (step %d/%d, %s/%s)%s",
		strings.ToUpper(string(rep.State)),
		userVisisbleCurrentStep, userVisibleTotalSteps,
		ByteCountBinaryUint(replicated), ByteCountBinaryUint(expected),
		sizeEstimationImpreciseNotice,
	)

	activeIndicator := " "
	if rep.BlockedOn == report.FsBlockedOnNothing &&
		(rep.State == report.FilesystemPlanning || rep.State == report.FilesystemStepping) {
		activeIndicator = "*"
	}
	t.AddIndent(1)
	t.Printf("%s %s %s ",
		activeIndicator,
		stringbuilder.RightPad(rep.Info.Name, maxFS, " "),
		status)

	next := ""
	if err := rep.Error(); err != nil {
		next = err.Err
	} else if rep.State != report.FilesystemDone {
		if nextStep := rep.NextStep(); nextStep != nil {
			if nextStep.IsIncremental() {
				next = fmt.Sprintf("next: %s => %s", nextStep.Info.From, nextStep.Info.To)
			} else {
				next = fmt.Sprintf("next: full send %s", nextStep.Info.To)
			}
			attribs := []string{}

			if nextStep.Info.Resumed {
				attribs = append(attribs, "resumed")
			}

			if len(attribs) > 0 {
				next += fmt.Sprintf(" (%s)", strings.Join(attribs, ", "))
			}
		} else {
			next = "" // individual FSes may still be in planning state
		}

	}
	t.Printf("%s", next)

	t.AddIndent(-1)
	t.Newline()
}

func renderReplicationReport(t *stringbuilder.B, rep *report.Report, history *bytesProgressHistory, fsfilter FilterFunc) {
	if rep == nil {
		t.Printf("...\n")
		return
	}

	if rep.WaitReconnectError != nil {
		t.PrintfDrawIndentedAndWrappedIfMultiline("Connectivity: %s", rep.WaitReconnectError)
		t.Newline()
	}
	if !rep.WaitReconnectSince.IsZero() {
		delta := time.Until(rep.WaitReconnectUntil).Round(time.Second)
		if rep.WaitReconnectUntil.IsZero() || delta > 0 {
			var until string
			if rep.WaitReconnectUntil.IsZero() {
				until = "waiting indefinitely"
			} else {
				until = fmt.Sprintf("hard fail in %s @ %s", delta, rep.WaitReconnectUntil)
			}
			t.PrintfDrawIndentedAndWrappedIfMultiline("Connectivity: reconnecting with exponential backoff (since %s) (%s)",
				rep.WaitReconnectSince, until)
		} else {
			t.PrintfDrawIndentedAndWrappedIfMultiline("Connectivity: reconnects reached hard-fail timeout @ %s", rep.WaitReconnectUntil)
		}
		t.Newline()
	}

	// TODO visualize more than the latest attempt by folding all attempts into one
	if len(rep.Attempts) == 0 {
		t.Printf("no attempts made yet")
		return
	} else {
		t.Printf("Attempt #%d", len(rep.Attempts))
		if len(rep.Attempts) > 1 {
			t.Printf(". Previous attempts failed with the following statuses:")
			t.AddIndentAndNewline(1)
			for i, a := range rep.Attempts[:len(rep.Attempts)-1] {
				t.PrintfDrawIndentedAndWrappedIfMultiline("#%d: %s (failed at %s) (ran %s)\n", i+1, a.State, a.FinishAt, a.FinishAt.Sub(a.StartAt))
			}
			t.AddIndentAndNewline(-1)
		} else {
			t.Newline()
		}
	}

	latest := rep.Attempts[len(rep.Attempts)-1]
	sort.Slice(latest.Filesystems, func(i, j int) bool {
		return latest.Filesystems[i].Info.Name < latest.Filesystems[j].Info.Name
	})

	// apply filter
	filtered := make([]*report.FilesystemReport, 0, len(latest.Filesystems))
	for _, fs := range latest.Filesystems {
		if !fsfilter(fs.Info.Name) {
			continue
		}
		filtered = append(filtered, fs)
	}
	latest.Filesystems = filtered

	t.Printf("Status: %s", latest.State)
	t.Newline()
	if !latest.FinishAt.IsZero() {
		t.Printf("Last Run: %s (lasted %s)\n", latest.FinishAt.Round(time.Second), latest.FinishAt.Sub(latest.StartAt).Round(time.Second))
	} else {
		t.Printf("Started: %s (lasting %s)\n", latest.StartAt.Round(time.Second), time.Since(latest.StartAt).Round(time.Second))
	}

	if latest.State == report.AttemptPlanningError {
		t.Printf("Problem: ")
		t.PrintfDrawIndentedAndWrappedIfMultiline("%s", latest.PlanError)
		t.Newline()
	} else if latest.State == report.AttemptFanOutError {
		t.Printf("Problem: one or more of the filesystems encountered errors")
		t.Newline()
	}

	if latest.State != report.AttemptPlanning && latest.State != report.AttemptPlanningError {
		// Draw global progress bar
		// Progress: [---------------]
		expected, replicated, containsInvalidSizeEstimates := latest.BytesSum()
		rate, changeCount := history.Update(replicated)
		eta := time.Duration(0)
		if rate > 0 {
			eta = time.Duration((float64(expected)-float64(replicated))/float64(rate)) * time.Second
		}

		if !latest.State.IsTerminal() {
			t.Write("Progress: ")
			t.DrawBar(50, replicated, expected, changeCount)
			t.Write(fmt.Sprintf(" %s / %s @ %s/s", ByteCountBinaryUint(replicated), ByteCountBinaryUint(expected), ByteCountBinary(rate)))
			if eta != 0 {
				t.Write(fmt.Sprintf(" (%s remaining)", humanizeDuration(eta)))
			}
			t.Newline()
		}
		if containsInvalidSizeEstimates {
			t.Write("NOTE: not all steps could be size-estimated, total estimate is likely imprecise!")
			t.Newline()
		}

		if len(latest.Filesystems) == 0 {
			t.Write("NOTE: no filesystems were considered for replication!")
			t.Newline()
		}

		var maxFSLen int
		for _, fs := range latest.Filesystems {
			if len(fs.Info.Name) > maxFSLen {
				maxFSLen = len(fs.Info.Name)
			}
		}
		for _, fs := range latest.Filesystems {
			printFilesystemStatus(t, fs, maxFSLen)
		}

	}
}

func humanizeDuration(duration time.Duration) string {
	days := int64(duration.Hours() / 24)
	hours := int64(math.Mod(duration.Hours(), 24))
	minutes := int64(math.Mod(duration.Minutes(), 60))
	seconds := int64(math.Mod(duration.Seconds(), 60))

	var parts []string

	force := false
	chunks := []int64{days, hours, minutes, seconds}
	for i, chunk := range chunks {
		if force || chunk > 0 {
			padding := 0
			if force {
				padding = 2
			}
			parts = append(parts, fmt.Sprintf("%*d%c", padding, chunk, "dhms"[i]))
			force = true
		}
	}

	return strings.Join(parts, " ")
}

func renderPrunerReport(t *stringbuilder.B, r *pruner.Report, fsfilter FilterFunc) {
	if r == nil {
		t.Printf("...\n")
		return
	}

	state, err := pruner.StateString(r.State)
	if err != nil {
		t.Printf("Status: %q (parse error: %q)\n", r.State, err)
		return
	}

	t.Printf("Status: %s", state)
	t.Newline()

	if r.Error != "" {
		t.Printf("Error: %s\n", r.Error)
	}

	type commonFS struct {
		*pruner.FSReport
		completed bool
	}
	all := make([]commonFS, 0, len(r.Pending)+len(r.Completed))
	for i := range r.Pending {
		all = append(all, commonFS{&r.Pending[i], false})
	}
	for i := range r.Completed {
		all = append(all, commonFS{&r.Completed[i], true})
	}

	// filter all
	filtered := make([]commonFS, 0, len(all))
	for _, fs := range all {
		if fsfilter(fs.FSReport.Filesystem) {
			filtered = append(filtered, fs)
		}
	}
	all = filtered

	switch state {
	case pruner.Plan:
		fallthrough
	case pruner.PlanErr:
		return
	}

	if len(all) == 0 {
		t.Printf("nothing to do\n")
		return
	}

	var totalDestroyCount, completedDestroyCount int
	var maxFSname int
	for _, fs := range all {
		totalDestroyCount += len(fs.DestroyList)
		if fs.completed {
			completedDestroyCount += len(fs.DestroyList)
		}
		if maxFSname < len(fs.Filesystem) {
			maxFSname = len(fs.Filesystem)
		}
	}

	// global progress bar
	if !state.IsTerminal() {
		progress := int(math.Round(80 * float64(completedDestroyCount) / float64(totalDestroyCount)))
		t.Write("Progress: ")
		t.Write("[")
		t.Write(stringbuilder.Times("=", progress))
		t.Write(">")
		t.Write(stringbuilder.Times("-", 80-progress))
		t.Write("]")
		t.Printf(" %d/%d snapshots", completedDestroyCount, totalDestroyCount)
		t.Newline()
	}

	sort.SliceStable(all, func(i, j int) bool {
		return strings.Compare(all[i].Filesystem, all[j].Filesystem) == -1
	})

	// Draw a table-like representation of 'all'
	for _, fs := range all {
		t.Write(stringbuilder.RightPad(fs.Filesystem, maxFSname, " "))
		t.Write(" ")
		if !fs.SkipReason.NotSkipped() {
			t.Printf("skipped: %s\n", fs.SkipReason)
			continue
		}
		if fs.LastError != "" {
			if strings.ContainsAny(fs.LastError, "\r\n") {
				t.Printf("ERROR:")
				t.PrintfDrawIndentedAndWrappedIfMultiline("%s\n", fs.LastError)
			} else {
				t.PrintfDrawIndentedAndWrappedIfMultiline("ERROR: %s\n", fs.LastError)
			}
			t.Newline()
			continue
		}

		pruneRuleActionStr := fmt.Sprintf("(destroy %d of %d snapshots)",
			len(fs.DestroyList), len(fs.SnapshotList))

		if fs.completed {
			t.Printf("Completed  %s\n", pruneRuleActionStr)
			continue
		}

		t.Write("Pending    ") // whitespace is padding 10
		if len(fs.DestroyList) == 1 {
			t.Write(fs.DestroyList[0].Name)
		} else {
			t.Write(pruneRuleActionStr)
		}
		t.Newline()
	}

}

func renderSnapperReport(t *stringbuilder.B, r *snapper.Report, fsfilter FilterFunc) {
	if r == nil {
		t.Printf("<no snapshotting report available>\n")
		return
	}
	t.Printf("Type: %s\n", r.Type)
	if r.Periodic != nil {
		renderSnapperReportPeriodic(t, r.Periodic, fsfilter)
	} else if r.Cron != nil {
		renderSnapperReportCron(t, r.Cron, fsfilter)
	} else {
		t.Printf("<no details available>")
	}
}

func renderSnapperReportPeriodic(t *stringbuilder.B, r *snapper.PeriodicReport, fsfilter FilterFunc) {
	t.Printf("Status: %s", r.State)
	t.Newline()

	if r.Error != "" {
		t.Printf("Error: %s\n", r.Error)
	}
	if !r.SleepUntil.IsZero() {
		t.Printf("Sleep until: %s\n", r.SleepUntil)
	}

	renderSnapperPlanReportFilesystem(t, r.Progress, fsfilter)
}

func renderSnapperReportCron(t *stringbuilder.B, r *snapper.CronReport, fsfilter FilterFunc) {
	t.Printf("State: %s\n", r.State)

	now := time.Now()
	if r.WakeupTime.After(now) {
		t.Printf("Sleep until: %s (%s remaining)\n", r.WakeupTime, r.WakeupTime.Sub(now).Round(time.Second))
	} else {
		t.Printf("Started: %s (lasting %s)\n", r.WakeupTime, now.Sub(r.WakeupTime).Round(time.Second))
	}

	renderSnapperPlanReportFilesystem(t, r.Progress, fsfilter)
}

func renderSnapperPlanReportFilesystem(t *stringbuilder.B, fss []*snapper.ReportFilesystem, fsfilter FilterFunc) {
	sort.Slice(fss, func(i, j int) bool {
		return strings.Compare(fss[i].Path, fss[j].Path) == -1
	})

	dur := func(d time.Duration) string {
		return d.Round(100 * time.Millisecond).String()
	}

	type row struct {
		path, state, duration, remainder, hookReport string
	}
	var widths struct {
		path, state, duration int
	}
	rows := make([]*row, 0, len(fss))
	for _, fs := range fss {
		if !fsfilter(fs.Path) {
			continue
		}
		r := &row{
			path:  fs.Path,
			state: fs.State.String(),
		}
		if fs.HooksHadError {
			r.hookReport = fs.Hooks // FIXME render here, not in daemon
		}
		switch fs.State {
		case snapper.SnapPending:
			r.duration = "..."
			r.remainder = ""
		case snapper.SnapStarted:
			r.duration = dur(time.Since(fs.StartAt))
			r.remainder = fmt.Sprintf("snap name: %q", fs.SnapName)
		case snapper.SnapDone:
			fallthrough
		case snapper.SnapError:
			r.duration = dur(fs.DoneAt.Sub(fs.StartAt))
			r.remainder = fmt.Sprintf("snap name: %q", fs.SnapName)
		}
		rows = append(rows, r)
		if len(r.path) > widths.path {
			widths.path = len(r.path)
		}
		if len(r.state) > widths.state {
			widths.state = len(r.state)
		}
		if len(r.duration) > widths.duration {
			widths.duration = len(r.duration)
		}
	}

	for _, r := range rows {
		path := stringbuilder.RightPad(r.path, widths.path, " ")
		state := stringbuilder.RightPad(r.state, widths.state, " ")
		duration := stringbuilder.RightPad(r.duration, widths.duration, " ")
		t.Printf("%s %s %s", path, state, duration)
		t.PrintfDrawIndentedAndWrappedIfMultiline(" %s", r.remainder)
		if r.hookReport != "" {
			t.AddIndent(1)
			t.Newline()
			t.Printf("%s", r.hookReport)
			t.AddIndent(-1)
		}
		t.Newline()
	}
}
