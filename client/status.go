package client

import (
	"fmt"
	"github.com/gdamore/tcell/termbox"
	"github.com/pkg/errors"
	"github.com/spf13/pflag"
	"github.com/zrepl/yaml-config"
	"github.com/zrepl/zrepl/cli"
	"github.com/zrepl/zrepl/daemon"
	"github.com/zrepl/zrepl/daemon/job"
	"github.com/zrepl/zrepl/daemon/pruner"
	"github.com/zrepl/zrepl/replication/report"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type byteProgressMeasurement struct {
	time time.Time
	val int64
}

type bytesProgressHistory struct {
	last   *byteProgressMeasurement // pointer as poor man's optional
	changeCount int
	lastChange time.Time
	bpsAvg float64
}

func (p *bytesProgressHistory) Update(currentVal int64) (bytesPerSecondAvg int64, changeCount int) {

	if p.last == nil {
		p.last = &byteProgressMeasurement{
			time: time.Now(),
			val: currentVal,
		}
		return 0, 0
	}

	if p.last.val != currentVal {
		p.changeCount++
		p.lastChange = time.Now()
	}

	if time.Now().Sub(p.lastChange) > 3 * time.Second {
		p.last = nil
		return 0, 0
	}


	deltaV := currentVal - p.last.val;
	deltaT := time.Now().Sub(p.last.time)
	rate := float64(deltaV) / deltaT.Seconds()

	factor := 0.3
	p.bpsAvg =  (1-factor) * p.bpsAvg + factor * rate

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

	replicationProgress map[string]*bytesProgressHistory // by job name
}

func newTui() tui {
	return tui{
		replicationProgress: make(map[string]*bytesProgressHistory, 0),
	}
}

func (t *tui) moveCursor(x, y int) {
	t.x += x
	t.y += y
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
			rem = idx+1
		}
		untilNewline := strings.TrimSpace(s[:rem])
		s = s[rem:]
		if len(untilNewline) == 0 {
			continue
		}
		b.WriteString(untilNewline)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func (t *tui) printfDrawIndentedAndWrappedIfMultiline(format string, a ...interface{}) {
	whole := fmt.Sprintf(format, a...)
	width, _ := termbox.Size()
	if !strings.ContainsAny(whole, "\n\r") && t.x + len(whole) <= width {
		t.printf(format, a...)
	} else {
		t.addIndent(1)
		t.newline()
		t.write(wrap(whole, width - INDENT_MULTIPLIER*t.indent))
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
}

var StatusCmd = &cli.Subcommand{
	Use:   "status",
	Short: "show job activity or dump as JSON for monitoring",
	SetupFlags: func(f *pflag.FlagSet) {
		f.BoolVar(&statusFlags.Raw, "raw", false, "dump raw status description from zrepl daemon")
	},
	Run: runStatus,
}

func runStatus(s *cli.Subcommand, args []string) error {
	httpc, err := controlHttpClient(s.Config().Global.Control.SockPath)
	if err != nil {
		return err
	}

	if statusFlags.Raw {
		resp, err := httpc.Get("http://unix"+daemon.ControlJobEndpointStatus)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			fmt.Fprintf(os.Stderr, "Received error response:\n")
			io.CopyN(os.Stderr, resp.Body, 4096)
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
		for _ = range ticker.C {
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
		keys := make([]string, len(t.report))
		i := 0
		for k, _ := range t.report {
			keys[i] = k
			i++
		}
		sort.Strings(keys)

		for _, k := range keys {
			v := t.report[k]
			if len(k) == 0 || daemon.IsInternalJobName(k) { //Internal job
				continue
			}
			t.setIndent(0)

			t.printf("Job: %s", k)
			t.setIndent(1)
			t.newline()
			t.printf("Type: %s", v.Type)
			t.setIndent(1)
			t.newline()

			if v.Type != job.TypePush && v.Type != job.TypePull {
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

			pushStatus, ok := v.JobSpecific.(*job.ActiveSideStatus)
			if !ok || pushStatus == nil {
				t.printf("ActiveSideStatus is null")
				t.newline()
				continue
			}

			t.printf("Replication:")
			t.newline()
			t.addIndent(1)
			t.renderReplicationReport(pushStatus.Replication, t.getReplicationProgresHistory(k))
			t.addIndent(-1)

			t.printf("Pruning Sender:")
			t.newline()
			t.addIndent(1)
			t.renderPrunerReport(pushStatus.PruningSender)
			t.addIndent(-1)

			t.printf("Pruning Receiver:")
			t.newline()
			t.addIndent(1)
			t.renderPrunerReport(pushStatus.PruningReceiver)
			t.addIndent(-1)

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
		delta := rep.WaitReconnectUntil.Sub(time.Now()).Round(time.Second)
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
			t.printf(". Previous attempts failed with the follwing statuses:")
			t.newline()
			t.addIndent(1)
			for i, a := range rep.Attempts[:len(rep.Attempts)-1] {
				t.printfDrawIndentedAndWrappedIfMultiline("#%d: %s (failed at %s) (ran %s)", i + 1, a.State, a.FinishAt, a.FinishAt.Sub(a.StartAt))
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
	if r.SleepUntil.After(time.Now()) {
		t.printf("Sleeping until %s (%s left)\n", r.SleepUntil, r.SleepUntil.Sub(time.Now()))
	}

	type commonFS struct {
		*pruner.FSReport
		completed bool
	}
	all := make([]commonFS, 0, len(r.Pending) + len(r.Completed))
	for i := range r.Pending {
		all = append(all, commonFS{&r.Pending[i], false})
	}
	for i := range r.Completed {
		all = append(all, commonFS{&r.Completed[i], true})
	}

	switch state {
	case pruner.Plan: fallthrough
	case pruner.PlanWait: fallthrough
	case pruner.ErrPerm:
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
	t.write(times("-", 80 - progress))
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
		if fs.LastError != "" {
			t.printf("ERROR (%d): %s\n", fs.ErrorCount, fs.LastError) // whitespace is padding
			continue
		}

		pruneRuleActionStr := fmt.Sprintf("(destroy %d of %d snapshots)",
			len(fs.DestroyList), len(fs.SnapshotList))

		if fs.completed {
			t.printf( "Completed  %s\n", pruneRuleActionStr)
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
	return str + times(pad, length-len(str))
}


func leftPad(str string, length int, pad string) string {
	if len(str) > length {
		return str[len(str)-length:]
	}
	return times(pad, length-len(str)) + str
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
	t.write( string(arrowPositions[changeCount%len(arrowPositions)]))
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
