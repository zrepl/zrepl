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
	"github.com/zrepl/zrepl/replication"
	"github.com/zrepl/zrepl/replication/fsrep"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type tui struct {
	x, y   int
	indent int

	lock   sync.Mutex //For report and error
	report map[string]job.Status
	err    error
}

func newTui() tui {
	return tui{}
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
			t.renderReplicationReport(pushStatus.Replication)
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

func (t *tui) renderReplicationReport(rep *replication.Report) {
	if rep == nil {
		t.printf("...\n")
		return
	}

	all := make([]*fsrep.Report, 0, len(rep.Completed)+len(rep.Pending) + 1)
	all = append(all, rep.Completed...)
	all = append(all, rep.Pending...)
	if rep.Active != nil {
		all = append(all, rep.Active)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].Filesystem < all[j].Filesystem
	})

	state, err := replication.StateString(rep.Status)
	if err != nil {
		t.printf("Status: %q (parse error: %q)\n", rep.Status, err)
		return
	}

	t.printf("Status: %s", state)
	t.newline()
	if rep.Problem != "" {
		t.printf("Problem: ")
		t.printfDrawIndentedAndWrappedIfMultiline("%s", rep.Problem)
		t.newline()
	}
	if rep.SleepUntil.After(time.Now()) && !state.IsTerminal() {
		t.printf("Sleeping until %s (%s left)\n", rep.SleepUntil, rep.SleepUntil.Sub(time.Now()))
	}

	if state != replication.Planning && state != replication.PlanningError {
		// Progress: [---------------]
		sumUpFSRep := func(rep *fsrep.Report) (transferred, total int64) {
			for _, s := range rep.Pending {
				transferred += s.Bytes
				total  += s.ExpectedBytes
			}
			for _, s := range rep.Completed {
				transferred += s.Bytes
				total += s.ExpectedBytes
			}
			return
		}
		var transferred, total int64
		for _, fs := range all {
			fstx, fstotal := sumUpFSRep(fs)
			transferred += fstx
			total += fstotal
		}
		t.write("Progress: ")
		t.drawBar(80, transferred, total)
		t.write(fmt.Sprintf(" %s / %s", ByteCountBinary(transferred), ByteCountBinary(total)))
		t.newline()
	}

	var maxFSLen int
	for _, fs := range all {
		if len(fs.Filesystem) > maxFSLen {
			maxFSLen = len(fs.Filesystem)
		}
	}
	for _, fs := range all {
		t.printFilesystemStatus(fs, fs == rep.Active, maxFSLen)
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

const snapshotIndent = 1
func calculateMaxFSLength(all []*fsrep.Report) (maxFS, maxStatus int) {
	for _, e := range all {
		if len(e.Filesystem) > maxFS {
			maxFS = len(e.Filesystem)
		}
		all2 := make([]*fsrep.StepReport, 0, len(e.Pending) + len(e.Completed))
		all2 = append(all2, e.Pending...)
		all2 = append(all2, e.Completed...)
		for _, e2 := range all2 {
			elen := len(e2.Problem) + len(e2.From) + len(e2.To) + 60 // random spacing, units, labels, etc
			if elen > maxStatus {
				maxStatus = elen
			}
		}
	}
	return
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

func (t *tui) drawBar(length int, bytes, totalBytes int64) {
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
	t.write(">")
	t.write(times("-", length-completedLength))
	t.write("]")
}

func StringStepState(s fsrep.StepState) string {
	switch s {
	case fsrep.StepReplicationReady: return "Ready"
	case fsrep.StepMarkReplicatedReady: return "MarkReady"
	case fsrep.StepCompleted: return "Completed"
	default:
		return fmt.Sprintf("UNKNOWN %d", s)
	}
}

func (t *tui) printFilesystemStatus(rep *fsrep.Report, active bool, maxFS int) {

	bytes := int64(0)
	totalBytes := int64(0)
	for _, s := range rep.Pending {
		bytes += s.Bytes
		totalBytes += s.ExpectedBytes
	}
	for _, s := range rep.Completed {
		bytes += s.Bytes
		totalBytes += s.ExpectedBytes
	}


	status := fmt.Sprintf("%s (step %d/%d, %s/%s)",
		rep.Status,
		len(rep.Completed), len(rep.Pending) + len(rep.Completed),
		ByteCountBinary(bytes), ByteCountBinary(totalBytes),

	)

	activeIndicator := " "
	if active {
		activeIndicator = "*"
	}
	t.printf("%s %s %s ",
		activeIndicator,
		rightPad(rep.Filesystem, maxFS, " "),
		status)

	next := ""
	if rep.Problem != "" {
		next = rep.Problem
	} else if len(rep.Pending) > 0 {
		if rep.Pending[0].From != "" {
			next = fmt.Sprintf("next: %s => %s", rep.Pending[0].From, rep.Pending[0].To)
		} else {
			next = fmt.Sprintf("next: %s (full)", rep.Pending[0].To)
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
