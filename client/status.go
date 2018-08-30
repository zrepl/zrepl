package client

import (
	"fmt"
	"github.com/mitchellh/mapstructure"
	"github.com/nsf/termbox-go"
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon"
	"github.com/zrepl/zrepl/replication"
	"github.com/zrepl/zrepl/replication/fsrep"
	"sort"
	"sync"
	"time"
)

type tui struct {
	x, y   int
	indent int

	lock   sync.Mutex //For report and error
	report map[string]interface{}
	err    error
}

func newTui() tui {
	return tui{}
}

func (t *tui) moveCursor(x, y int) {
	t.x += x
	t.y += y
}

func (t *tui) moveLine(dl int, col int) {
	t.y += dl
	t.x = t.indent*4 + col
}

func (t *tui) write(text string) {
	for _, c := range text {
		termbox.SetCell(t.x, t.y, c, termbox.ColorDefault, termbox.ColorDefault)
		t.x += 1
	}
}

func (t *tui) printf(text string, a ...interface{}) {
	t.write(fmt.Sprintf(text, a...))
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

func RunStatus(config config.Config, args []string) error {
	httpc, err := controlHttpClient(config.Global.Control.SockPath)
	if err != nil {
		return err
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
		m := make(map[string]interface{})

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
			if len(k) == 0 || k[0] == '_' { //Internal job
				continue
			}
			t.setIndent(0)

			t.printf("Job: %s", k)
			t.setIndent(1)
			t.newline()

			if v == nil {
				t.printf("No report generated yet")
				t.newline()
				continue
			}
			rep := replication.Report{}
			err := mapstructure.Decode(v, &rep)
			if err != nil {
				t.printf("Failed to decode report: %s", err.Error())
				t.newline()
				continue
			}
			t.printf("Status:  %s", rep.Status)
			t.newline()
			if rep.Problem != "" {
				t.printf("Problem: %s", rep.Problem)
				t.newline()
			}

			maxNameLength := calculateMaxFilesystemAndVersionNameLength(&rep)

			for _, fs := range rep.Completed {
				printFilesystem(fs, t, false, maxNameLength)
			}
			if rep.Active != nil {
				printFilesystem(rep.Active, t, true, maxNameLength)
			}
			for _, fs := range rep.Pending {
				printFilesystem(fs, t, false, maxNameLength)
			}

		}
	}
	termbox.Flush()
}

const snapshotIndent = 1
func calculateMaxFilesystemAndVersionNameLength(report *replication.Report) int {
	all := append(report.Completed, report.Active)
	all = append(all, report.Pending...)
	maxLen := 0
	for _, e := range all {
		if e == nil { //active can be nil
			continue
		}
		if len(e.Filesystem) > maxLen {
			maxLen = len(e.Filesystem)
		}
		all2 := append(e.Pending, e.Completed...)
		for _, e2 := range all2 {
			if len(e2.To) + snapshotIndent > maxLen {
				maxLen = len(e2.To) + snapshotIndent
			}
		}
	}
	return maxLen
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

func (t *tui) drawBar(name string, maxNameLength int, status string, bytes int64, totalBytes int64) {
	t.write(rightPad(name, maxNameLength + 1, " "))
	t.write(" ")
	t.write(rightPad(status, 14, " "))
	t.write(" ")

	if totalBytes > 0 {
		length := 50
		completedLength := int(int64(length) * bytes / totalBytes)
		if completedLength > length {
			completedLength = length
		}

		t.write("[")
		t.write(times("=", completedLength))
		t.write(">")
		t.write(times("-", length-completedLength))
		t.write("]")

		t.write(" ")
		t.write(rightPad(ByteCountBinary(bytes) + "/" + ByteCountBinary(totalBytes), 20, " "))
	}

	t.newline()
}

func StringStepState(s fsrep.StepState) string {
	switch s {
	case fsrep.StepReplicationReady: return "Ready"
	case fsrep.StepReplicationRetry: return "Retry"
	case fsrep.StepMarkReplicatedReady: return "MarkReady"
	case fsrep.StepMarkReplicatedRetry: return "MarkRetry"
	case fsrep.StepPermanentError: return "PermanentError"
	case fsrep.StepCompleted: return "Completed"
	default:
		return fmt.Sprintf("UNKNOWN %d", s)
	}
}

func printFilesystem(rep *fsrep.Report, t *tui, versions bool, maxNameLength int) {
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

	t.drawBar(rep.Filesystem, maxNameLength, rep.Status, bytes, totalBytes)
	if rep.Problem != "" {
		t.addIndent(1)
		t.printf("Problem: %s", rep.Problem)
		t.newline()
		t.addIndent(-1)
	}
	if versions && len(rep.Pending) > 0 {
		v := rep.Pending[0]
		t.drawBar(times(" ", snapshotIndent) + v.To, maxNameLength, StringStepState(v.Status), v.Bytes, v.ExpectedBytes)
	}
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
