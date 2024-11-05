package status

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-isatty"
	"github.com/pkg/errors"
	"github.com/rivo/tview"

	"github.com/zrepl/zrepl/internal/client/status/viewmodel"
)

func legacy(c Client, flag statusFlags) error {

	// Set this so we don't overwrite the default terminal colors
	// See https://github.com/rivo/tview/blob/master/styles.go
	tview.Styles.PrimitiveBackgroundColor = tcell.ColorDefault
	tview.Styles.ContrastBackgroundColor = tcell.ColorDefault
	tview.Styles.PrimaryTextColor = tcell.ColorDefault
	tview.Styles.BorderColor = tcell.ColorDefault
	app := tview.NewApplication()

	textView := tview.NewTextView()
	textView.SetWrap(true)
	textView.SetScrollable(true) // so that it allows us to set scroll position
	// textView.SetScrollBarVisibility(tview.ScrollBarNever)

	app.SetRoot(textView, true)

	width := (1 << 31) - 1
	wrap := false
	if isatty.IsTerminal(os.Stdout.Fd()) {
		wrap = true
		screen, err := tcell.NewScreen()
		if err != nil {
			return errors.Wrap(err, "get terminal dimensions")
		}
		if err := screen.Init(); err != nil {
			return errors.Wrap(err, "init screen")
		}
		width, _ = screen.Size()
		screen.Fini()
	}

	paramsMtx := &sync.Mutex{}
	params := viewmodel.Params{
		Report:                  nil,
		ReportFetchError:        nil,
		SelectedJob:             nil,
		FSFilter:                func(s string) bool { return true },
		DetailViewWidth:         width,
		DetailViewWrap:          wrap,
		ShortKeybindingOverview: "",
	}

	redraw := func() {
		textView.Clear()

		paramsMtx.Lock()
		defer paramsMtx.Unlock()

		if params.ReportFetchError != nil {
			fmt.Fprintln(textView, params.ReportFetchError.Error())
		} else if params.Report != nil {
			m := viewmodel.New()
			m.Update(params)
			for _, j := range m.Jobs() {
				if flag.Job != "" && j.Name() != flag.Job {
					continue
				}
				params.SelectedJob = j
				m.Update(params)
				fmt.Fprintln(textView, m.SelectedJob().FullDescription())
				if flag.Job != "" {
					break
				} else {
					hline := strings.Repeat("-", params.DetailViewWidth)
					fmt.Fprintln(textView, hline)
				}
			}
		} else {
			fmt.Fprintln(textView, "waiting for request results")
		}
		textView.ScrollToBeginning()
	}

	app.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
		// sync resizes to `params`
		paramsMtx.Lock()
		_, _, newWidth, _ := textView.GetInnerRect()
		if newWidth != params.DetailViewWidth {
			params.DetailViewWidth = newWidth
			app.QueueUpdateDraw(redraw)
		}
		paramsMtx.Unlock()

		textView.ScrollToBeginning() // has the effect of inhibiting user scrolls
		return false
	})

	go func() {
		defer func() {
			if err := recover(); err != nil {
				app.Suspend(func() {
					panic(err)
				})
			}
		}()
		for {
			st, err := c.Status()
			paramsMtx.Lock()
			params.Report = st.Jobs
			params.ReportFetchError = err
			paramsMtx.Unlock()

			app.QueueUpdateDraw(redraw)

			time.Sleep(flag.Delay)
		}
	}()

	return app.Run()
}
