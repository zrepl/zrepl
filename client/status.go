package client

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	// tcell is the termbox-compatbile library for abstracting away escape sequences, etc.
	// as of tcell#252, the number of default distributed terminals is relatively limited
	// additional terminal definitions can be included via side-effect import
	// See https://github.com/gdamore/tcell/blob/master/terminfo/base/base.go
	// See https://github.com/gdamore/tcell/issues/252#issuecomment-533836078
	"github.com/gdamore/tcell/termbox"
	_ "github.com/gdamore/tcell/terminfo/s/screen" // tmux on FreeBSD 11 & 12 without ncurses

	"github.com/pkg/errors"
	"github.com/spf13/pflag"
	"github.com/zrepl/yaml-config"

	"github.com/zrepl/zrepl/cli"
	"github.com/zrepl/zrepl/daemon"
	"github.com/zrepl/zrepl/daemon/job"
	"github.com/zrepl/zrepl/daemon/pruner"
	"github.com/zrepl/zrepl/daemon/snapper"
	"github.com/zrepl/zrepl/replication/report"
)

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

type tui struct {
	x, y   int
	indent int

	lock   sync.Mutex //For report and error
	report map[string]job.Status
	err    error

	jobFilter string

	replicationProgress map[string]*bytesProgressHistory // by job name
}

func newTui() tui {
	return tui{
		replicationProgress: make(map[string]*bytesProgressHistory),
	}
}

const INDENT_MULTIPLIER = 4

func (t *tui) moveLine(dl int, col int) {
	t.y += dl
	t.x = t.indent*INDENT_MULTIPLIER + col
}

func (t *tui) write(text string) {
	for _, c := range text {
		if c == '\n' {
			t.newline()
			continue
		}
		termbox.SetCell(t.x, t.y, c, termbox.ColorDefault, termbox.ColorDefault)
		t.x += 1
	}
}

func (t *tui) printf(text string, a ...interface{}) {
	t.write(fmt.Sprintf(text, a...))
}

func wrap(s string, width int) string {
	var b strings.Builder
	for len(s) > 0 {
		rem := width
		if rem > len(s) {
			rem = len(s)
		}
		if idx := strings.IndexAny(s, "\n\r"); idx != -1 && idx < rem {
			rem = idx + 1
		}
		untilNewline := strings.TrimRight(s[:rem], "\n\r")
		s = s[rem:]
		if len(untilNewline) == 0 {
			continue
		}
		b.WriteString(untilNewline)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n\r")
}

func (t *tui) printfDrawIndentedAndWrappedIfMultiline(format string, a ...interface{}) {
	whole := fmt.Sprintf(format, a...)
	width, _ := termbox.Size()
	if !strings.ContainsAny(whole, "\n\r") && t.x+len(whole) <= width {
		t.printf(format, a...)
	} else {
		t.addIndent(1)
		t.newline()
		t.write(wrap(whole, width-INDENT_MULTIPLIER*t.indent))
		t.addIndent(-1)
	}
}

func (t *tui) newline() {
	t.moveLine(1, 0)
}

func (t *tui) setIndent(indent int) {
	t.indent = indent
	t.moveLine(0, 0)
}

func (t *tui) addIndent(indent int) {
	t.indent += indent
	t.moveLine(0, 0)
}

var statusFlags struct {
	Raw bool
	Job string
}

var StatusCmd = &cli.Subcommand{
	Use:   "status",
	Short: "show job activity or dump as JSON for monitoring",
	SetupFlags: func(f *pflag.FlagSet) {
		f.BoolVar(&statusFlags.Raw, "raw", false, "dump raw status description from zrepl daemon")
		f.StringVar(&statusFlags.Job, "job", "", "only dump specified job")
	},
	Run: runStatus,
}

func runStatus(s *cli.Subcommand, args []string) error {
	httpc, err := controlHttpClient(s.Config().Global.Control.SockPath)
	if err != nil {
		return err
	}

	if statusFlags.Raw {
		resp, err := httpc.Get("http://unix" + daemon.ControlJobEndpointStatus)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			fmt.Fprintf(os.Stderr, "Received error response:\n")
			_, err := io.CopyN(os.Stderr, resp.Body, 4096)
			if err != nil {
				return err
			}
			return errors.Errorf("exit")
		}
		if _, err := io.Copy(os.Stdout, resp.Body); err != nil {
			return err
		}
		return nil
	}

	t := newTui()
	t.lock.Lock()
	t.err = errors.New("Got no report yet")
	t.lock.Unlock()
	t.jobFilter = statusFlags.Job

	err = termbox.Init()
	if err != nil {
		return err
	}
	defer termbox.Close()

	update := func() {
		m := make(map[string]job.Status)

		err2 := jsonRequestResponse(httpc, daemon.ControlJobEndpointStatus,
			struct{}{},
			&m,
		)

		t.lock.Lock()
		t.err = err2
		t.report = m
		t.lock.Unlock()
		t.draw()
	}
	update()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	go func() {
		for range ticker.C {
			update()
		}
	}()

	termbox.HideCursor()
	termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)

loop:
	for {
		switch ev := termbox.PollEvent(); ev.Type {
		case termbox.EventKey:
			switch ev.Key {
			case termbox.KeyEsc:
				break loop
			case termbox.KeyCtrlC:
				break loop
			}
		case termbox.EventResize:
			t.draw()
		}
	}

	return nil

}

func (t *tui) getReplicationProgresHistory(jobName string) *bytesProgressHistory {
	p, ok := t.replicationProgress[jobName]
	if !ok {
		p = &bytesProgressHistory{}
		t.replicationProgress[jobName] = p
	}
	return p
}

func (t *tui) draw() {
	t.lock.Lock()
	defer t.lock.Unlock()

	termbox.Clear(termbox.ColorDefault, termbox.ColorDefault)
	t.x = 0
	t.y = 0
	t.indent = 0

	if t.err != nil {
		t.write(t.err.Error())
	} else {
		//Iterate over map in alphabetical order
		keys := make([]string, 0, len(t.report))
		for k := range t.report {
			if len(k) == 0 || daemon.IsInternalJobName(k) { //Internal job
				continue
			}
			if t.jobFilter != "" && k != t.jobFilter {
				continue
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)

		if len(keys) == 0 {
			t.setIndent(0)
			t.printf("no jobs to display")
			t.newline()
			termbox.Flush()
			return
		}

		for _, k := range keys {
			v := t.report[k]

			t.setIndent(0)

			t.printf("Job: %s", k)
			t.setIndent(1)
			t.newline()
			t.printf("Type: %s", v.Type)
			t.setIndent(1)
			t.newline()

			if v.Type == job.TypePush || v.Type == job.TypePull {
				activeStatus, ok := v.JobSpecific.(*job.ActiveSideStatus)
				if !ok || activeStatus == nil {
					t.printf("ActiveSideStatus is null")
					t.newline()
					continue
				}

				t.printf("Replication:")
				t.newline()
				t.addIndent(1)
				t.renderReplicationReport(activeStatus.Replication, t.getReplicationProgresHistory(k))
				t.addIndent(-1)

				t.printf("Pruning Sender:")
				t.newline()
				t.addIndent(1)
				t.renderPrunerReport(activeStatus.PruningSender)
				t.addIndent(-1)

				t.printf("Pruning Receiver:")
				t.newline()
				t.addIndent(1)
				t.renderPrunerReport(activeStatus.PruningReceiver)
				t.addIndent(-1)

				if v.Type == job.TypePush {
					t.printf("Snapshotting:")
					t.newline()
					t.addIndent(1)
					t.renderSnapperReport(activeStatus.Snapshotting)
					t.addIndent(-1)
				}

			} else if v.Type == job.TypeSnap {
				snapStatus, ok := v.JobSpecific.(*job.SnapJobStatus)
				if !ok || snapStatus == nil {
					t.printf("SnapJobStatus is null")
					t.newline()
					continue
				}
				t.printf("Pruning snapshots:")
				t.newline()
				t.addIndent(1)
				t.renderPrunerReport(snapStatus.Pruning)
				t.addIndent(-1)
				t.printf("Snapshotting:")
				t.newline()
				t.addIndent(1)
				t.renderSnapperReport(snapStatus.Snapshotting)
				t.addIndent(-1)
			} else if v.Type == job.TypeSource {

				st := v.JobSpecific.(*job.PassiveStatus)
				t.printf("Snapshotting:\n")
				t.addIndent(1)
				t.renderSnapperReport(st.Snapper)
				t.addIndent(-1)

			} else {
				t.printf("No status representation for job type '%s', dumping as YAML", v.Type)
				t.newline()
				asYaml, err := yaml.Marshal(v.JobSpecific)
				if err != nil {
					t.printf("Error marshaling status to YAML: %s", err)
					t.newline()
					continue
				}
				t.write(string(asYaml))
				t.newline()
				continue
			}
		}
	}
	termbox.Flush()
}

func (t *tui) renderReplicationReport(rep *report.Report, history *bytesProgressHistory) {
	if rep == nil {
		t.printf("...\n")
		return
	}

	if rep.WaitReconnectError != nil {
		t.printfDrawIndentedAndWrappedIfMultiline("Connectivity: %s", rep.WaitReconnectError)
		t.newline()
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
			t.printfDrawIndentedAndWrappedIfMultiline("Connectivity: reconnecting with exponential backoff (since %s) (%s)",
				rep.WaitReconnectSince, until)
		} else {
			t.printfDrawIndentedAndWrappedIfMultiline("Connectivity: reconnects reached hard-fail timeout @ %s", rep.WaitReconnectUntil)
		}
		t.newline()
	}

	// TODO visualize more than the latest attempt by folding all attempts into one
	if len(rep.Attempts) == 0 {
		t.printf("no attempts made yet")
		return
	} else {
		t.printf("Attempt #%d", len(rep.Attempts))
		if len(rep.Attempts) > 1 {
			t.printf(". Previous attempts failed with the following statuses:")
			t.newline()
			t.addIndent(1)
			for i, a := range rep.Attempts[:len(rep.Attempts)-1] {
				t.printfDrawIndentedAndWrappedIfMultiline("#%d: %s (failed at %s) (ran %s)", i+1, a.State, a.FinishAt, a.FinishAt.Sub(a.StartAt))
				t.newline()
			}
			t.addIndent(-1)
		} else {
			t.newline()
		}
	}

	latest := rep.Attempts[len(rep.Attempts)-1]
	sort.Slice(latest.Filesystems, func(i, j int) bool {
		return latest.Filesystems[i].Info.Name < latest.Filesystems[j].Info.Name
	})

	t.printf("Status: %s", latest.State)
	t.newline()
	if latest.State == report.AttemptPlanningError {
		t.printf("Problem: ")
		t.printfDrawIndentedAndWrappedIfMultiline("%s", latest.PlanError)
		t.newline()
	} else if latest.State == report.AttemptFanOutError {
		t.printf("Problem: one or more of the filesystems encountered errors")
		t.newline()
	}

	if latest.State != report.AttemptPlanning && latest.State != report.AttemptPlanningError {
		// Draw global progress bar
		// Progress: [---------------]
		expected, replicated := latest.BytesSum()
		rate, changeCount := history.Update(replicated)
		t.write("Progress: ")
		t.drawBar(50, replicated, expected, changeCount)
		t.write(fmt.Sprintf(" %s / %s @ %s/s", ByteCountBinary(replicated), ByteCountBinary(expected), ByteCountBinary(rate)))
		t.newline()

		var maxFSLen int
		for _, fs := range latest.Filesystems {
			if len(fs.Info.Name) > maxFSLen {
				maxFSLen = len(fs.Info.Name)
			}
		}
		for _, fs := range latest.Filesystems {
			t.printFilesystemStatus(fs, false, maxFSLen) // FIXME bring 'active' flag back
		}

	}

}

func (t *tui) renderPrunerReport(r *pruner.Report) {
	if r == nil {
		t.printf("...\n")
		return
	}

	state, err := pruner.StateString(r.State)
	if err != nil {
		t.printf("Status: %q (parse error: %q)\n", r.State, err)
		return
	}

	t.printf("Status: %s", state)
	t.newline()

	if r.Error != "" {
		t.printf("Error: %s\n", r.Error)
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

	switch state {
	case pruner.Plan:
		fallthrough
	case pruner.PlanErr:
		return
	}

	if len(all) == 0 {
		t.printf("nothing to do\n")
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
	progress := int(math.Round(80 * float64(completedDestroyCount) / float64(totalDestroyCount)))
	t.write("Progress: ")
	t.write("[")
	t.write(times("=", progress))
	t.write(">")
	t.write(times("-", 80-progress))
	t.write("]")
	t.printf(" %d/%d snapshots", completedDestroyCount, totalDestroyCount)
	t.newline()

	sort.SliceStable(all, func(i, j int) bool {
		return strings.Compare(all[i].Filesystem, all[j].Filesystem) == -1
	})

	// Draw a table-like representation of 'all'
	for _, fs := range all {
		t.write(rightPad(fs.Filesystem, maxFSname, " "))
		t.write(" ")
		if !fs.SkipReason.NotSkipped() {
			t.printf("skipped: %s\n", fs.SkipReason)
			continue
		}
		if fs.LastError != "" {
			if strings.ContainsAny(fs.LastError, "\r\n") {
				t.printf("ERROR:")
				t.printfDrawIndentedAndWrappedIfMultiline("%s\n", fs.LastError)
			} else {
				t.printfDrawIndentedAndWrappedIfMultiline("ERROR: %s\n", fs.LastError)
			}
			t.newline()
			continue
		}

		pruneRuleActionStr := fmt.Sprintf("(destroy %d of %d snapshots)",
			len(fs.DestroyList), len(fs.SnapshotList))

		if fs.completed {
			t.printf("Completed  %s\n", pruneRuleActionStr)
			continue
		}

		t.write("Pending    ") // whitespace is padding 10
		if len(fs.DestroyList) == 1 {
			t.write(fs.DestroyList[0].Name)
		} else {
			t.write(pruneRuleActionStr)
		}
		t.newline()
	}

}

func (t *tui) renderSnapperReport(r *snapper.Report) {
	if r == nil {
		t.printf("<snapshot type does not have a report>\n")
		return
	}

	t.printf("Status: %s", r.State)
	t.newline()

	if r.Error != "" {
		t.printf("Error: %s\n", r.Error)
	}
	if !r.SleepUntil.IsZero() {
		t.printf("Sleep until: %s\n", r.SleepUntil)
	}

	sort.Slice(r.Progress, func(i, j int) bool {
		return strings.Compare(r.Progress[i].Path, r.Progress[j].Path) == -1
	})

	t.addIndent(1)
	defer t.addIndent(-1)
	dur := func(d time.Duration) string {
		return d.Round(100 * time.Millisecond).String()
	}

	type row struct {
		path, state, duration, remainder, hookReport string
	}
	var widths struct {
		path, state, duration int
	}
	rows := make([]*row, len(r.Progress))
	for i, fs := range r.Progress {
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
		rows[i] = r
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
		path := rightPad(r.path, widths.path, " ")
		state := rightPad(r.state, widths.state, " ")
		duration := rightPad(r.duration, widths.duration, " ")
		t.printf("%s %s %s", path, state, duration)
		t.printfDrawIndentedAndWrappedIfMultiline(" %s", r.remainder)
		if r.hookReport != "" {
			t.printfDrawIndentedAndWrappedIfMultiline("%s", r.hookReport)
		}
		t.newline()
	}

}

func times(str string, n int) (out string) {
	for i := 0; i < n; i++ {
		out += str
	}
	return
}

func rightPad(str string, length int, pad string) string {
	if len(str) > length {
		return str[:length]
	}
	return str + strings.Repeat(pad, length-len(str))
}

var arrowPositions = `>\|/`

// changeCount = 0 indicates stall / no progresss
func (t *tui) drawBar(length int, bytes, totalBytes int64, changeCount int) {
	var completedLength int
	if totalBytes > 0 {
		completedLength = int(int64(length) * bytes / totalBytes)
		if completedLength > length {
			completedLength = length
		}
	} else if totalBytes == bytes {
		completedLength = length
	}

	t.write("[")
	t.write(times("=", completedLength))
	t.write(string(arrowPositions[changeCount%len(arrowPositions)]))
	t.write(times("-", length-completedLength))
	t.write("]")
}

func (t *tui) printFilesystemStatus(rep *report.FilesystemReport, active bool, maxFS int) {

	expected, replicated := rep.BytesSum()
	status := fmt.Sprintf("%s (step %d/%d, %s/%s)",
		strings.ToUpper(string(rep.State)),
		rep.CurrentStep, len(rep.Steps),
		ByteCountBinary(replicated), ByteCountBinary(expected),
	)

	activeIndicator := " "
	if active {
		activeIndicator = "*"
	}
	t.printf("%s %s %s ",
		activeIndicator,
		rightPad(rep.Info.Name, maxFS, " "),
		status)

	next := ""
	if err := rep.Error(); err != nil {
		next = err.Err
	} else if rep.State != report.FilesystemDone {
		if nextStep := rep.NextStep(); nextStep != nil {
			if nextStep.IsIncremental() {
				next = fmt.Sprintf("next: %s => %s", nextStep.Info.From, nextStep.Info.To)
			} else {
				next = fmt.Sprintf("next: %s (full)", nextStep.Info.To)
			}
		} else {
			next = "" // individual FSes may still be in planning state
		}

	}
	t.printfDrawIndentedAndWrappedIfMultiline("%s", next)

	t.newline()
}

func ByteCountBinary(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
