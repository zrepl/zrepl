package status

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell"
	"github.com/rivo/tview"

	"github.com/zrepl/zrepl/client/status.v2/viewmodel"
)

func interactive(c Client, flag statusFlags) error {

	// TODO look into https://gitlab.com/tslocum/cview/blob/master/FORK.md

	app := tview.NewApplication()

	jobDetailSplit := tview.NewFlex()
	jobMenu := tview.NewTreeView()
	jobMenuRoot := tview.NewTreeNode("jobs")
	jobMenuRoot.SetSelectable(true)
	jobMenu.SetRoot(jobMenuRoot).SetCurrentNode(jobMenuRoot)
	jobTextDetail := tview.NewTextView().SetWrap(false)

	jobMenu.SetBorder(true)
	jobTextDetail.SetBorder(true)

	toolbarSplit := tview.NewFlex().SetDirection(tview.FlexRow)
	inputBarContainer := tview.NewFlex()
	fsFilterInput := tview.NewInputField()
	fsFilterInput.SetBorder(false)
	inputBarContainer.AddItem(tview.NewTextView().SetText("[::b]FILTER ").SetDynamicColors(true), 7, 1, false)
	inputBarContainer.AddItem(fsFilterInput, 0, 10, false)
	toolbarSplit.AddItem(inputBarContainer, 1, 0, false)
	toolbarSplit.AddItem(jobDetailSplit, 0, 10, false)

	bottombar := tview.NewFlex().SetDirection(tview.FlexColumn)
	bottombarDateView := tview.NewTextView()
	bottombar.AddItem(bottombarDateView, len(time.Now().String()), 0, false)
	bottomBarStatus := tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignRight)
	bottombar.AddItem(bottomBarStatus, 0, 10, false)
	toolbarSplit.AddItem(bottombar, 1, 0, false)

	tabbableWithJobMenu := []tview.Primitive{jobMenu, jobTextDetail, fsFilterInput}
	tabbableWithoutJobMenu := []tview.Primitive{jobTextDetail, fsFilterInput}
	var tabbable []tview.Primitive
	tabbableActiveIndex := 0
	tabbableRedraw := func() {
		if len(tabbable) == 0 {
			app.SetFocus(nil)
			return
		}
		if tabbableActiveIndex >= len(tabbable) {
			app.SetFocus(tabbable[0])
			return
		}
		app.SetFocus(tabbable[tabbableActiveIndex])
	}
	tabbableCycle := func() {
		if len(tabbable) == 0 {
			return
		}
		tabbableActiveIndex = (tabbableActiveIndex + 1) % len(tabbable)
		app.SetFocus(tabbable[tabbableActiveIndex])
		tabbableRedraw()
	}

	jobMenuVisisble := false
	reconfigureJobDetailSplit := func(setJobMenuVisible bool) {
		if jobMenuVisisble == setJobMenuVisible {
			return
		}
		jobMenuVisisble = setJobMenuVisible
		if setJobMenuVisible {
			jobDetailSplit.RemoveItem(jobTextDetail)
			jobDetailSplit.AddItem(jobMenu, 0, 1, true)
			jobDetailSplit.AddItem(jobTextDetail, 0, 5, false)
			tabbable = tabbableWithJobMenu
		} else {
			jobDetailSplit.RemoveItem(jobMenu)
			tabbable = tabbableWithoutJobMenu
		}
		tabbableRedraw()
	}

	showModal := func(m *tview.Modal, modalDoneFunc func(idx int, label string)) {
		preModalFocus := app.GetFocus()
		m.SetDoneFunc(func(idx int, label string) {
			if modalDoneFunc != nil {
				modalDoneFunc(idx, label)
			}
			app.SetRoot(toolbarSplit, true)
			app.SetFocus(preModalFocus)
			app.Draw()
		})
		app.SetRoot(m, true)
		app.Draw()
	}

	app.SetRoot(toolbarSplit, true)
	// initial focus
	tabbableActiveIndex = len(tabbable)
	tabbableCycle()
	reconfigureJobDetailSplit(true)

	m := viewmodel.New()
	params := &viewmodel.Params{
		Report:                  nil,
		SelectedJob:             nil,
		FSFilter:                func(_ string) bool { return true },
		DetailViewWidth:         100,
		DetailViewWrap:          false,
		ShortKeybindingOverview: "[::b]<TAB>[::-] switch panes  [::b]Shift+M[::-] toggle navbar  [::b]Shift+S[::-] signal job [::b]</>[::-] filter filesystems",
	}
	paramsMtx := &sync.Mutex{}
	var redraw func()
	viewmodelupdate := func(cb func(*viewmodel.Params)) {
		paramsMtx.Lock()
		defer paramsMtx.Unlock()
		cb(params)
		m.Update(*params)
	}
	redraw = func() {
		jobs := m.Jobs()
		if flag.Job != "" {
			job_found := false
			for _, job := range jobs {
				if strings.Compare(flag.Job, job.Name()) == 0 {
					jobs = []*viewmodel.Job{job}
					job_found = true
					break
				}
			}
			if !job_found {
				jobs = nil
			}
		}
		redrawJobsList := false
		var selectedJobN *tview.TreeNode
		if len(jobMenuRoot.GetChildren()) == len(jobs) {
			for i, jobN := range jobMenuRoot.GetChildren() {
				if jobN.GetReference().(*viewmodel.Job) != jobs[i] {
					redrawJobsList = true
					break
				}
				if jobN.GetReference().(*viewmodel.Job) == m.SelectedJob() {
					selectedJobN = jobN
				}
			}
		} else {
			redrawJobsList = true
		}
		if redrawJobsList {
			selectedJobN = nil
			children := make([]*tview.TreeNode, len(jobs))
			for i := range jobs {
				jobN := tview.NewTreeNode(jobs[i].JobTreeTitle()).
					SetReference(jobs[i]).
					SetSelectable(true)
				children[i] = jobN
				jobN.SetSelectedFunc(func() {
					viewmodelupdate(func(p *viewmodel.Params) {
						p.SelectedJob = jobN.GetReference().(*viewmodel.Job)
					})
				})
				if jobs[i] == m.SelectedJob() {
					selectedJobN = jobN
				}
			}
			jobMenuRoot.SetChildren(children)
		}

		if selectedJobN != nil && jobMenu.GetCurrentNode() != selectedJobN {
			jobMenu.SetCurrentNode(selectedJobN)
		} else if selectedJobN == nil {
			// select something, otherwise selection breaks (likely bug in tview)
			jobMenu.SetCurrentNode(jobMenuRoot)
		}

		if selJ := m.SelectedJob(); selJ != nil {
			jobTextDetail.SetText(selJ.FullDescription())
		} else {
			jobTextDetail.SetText("please select a job")
		}

		bottombardatestring := m.DateString()
		bottombarDateView.SetText(bottombardatestring)
		bottombar.ResizeItem(bottombarDateView, len(bottombardatestring), 0)

		bottomBarStatus.SetText(m.BottomBarStatus())

		app.Draw()

	}

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
			viewmodelupdate(func(p *viewmodel.Params) {
				p.Report = st.Jobs
				p.ReportFetchError = err
			})
			app.QueueUpdateDraw(redraw)

			time.Sleep(flag.Delay)
		}
	}()

	jobMenu.SetChangedFunc(func(jobN *tview.TreeNode) {
		viewmodelupdate(func(p *viewmodel.Params) {
			p.SelectedJob, _ = jobN.GetReference().(*viewmodel.Job)
		})
		redraw()
		jobTextDetail.ScrollToBeginning()
	})
	jobMenu.SetSelectedFunc(func(jobN *tview.TreeNode) {
		app.SetFocus(jobTextDetail)
	})

	app.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
		viewmodelupdate(func(p *viewmodel.Params) {
			_, _, p.DetailViewWidth, _ = jobTextDetail.GetInnerRect()
		})
		return false
	})

	app.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if e.Key() == tcell.KeyTab {
			tabbableCycle()
			return nil
		}

		if e.Key() == tcell.KeyRune && app.GetFocus() == fsFilterInput {
			return e
		}

		if e.Key() == tcell.KeyRune && e.Rune() == '/' {
			if app.GetFocus() != fsFilterInput {
				app.SetFocus(fsFilterInput)
			}
			return e
		}

		if e.Key() == tcell.KeyRune && e.Rune() == 'M' {
			reconfigureJobDetailSplit(!jobMenuVisisble)
			return nil
		}

		if e.Key() == tcell.KeyRune && e.Rune() == 'S' {
			job, ok := jobMenu.GetCurrentNode().GetReference().(*viewmodel.Job)
			if !ok {
				return nil
			}
			signals := []string{"wakeup", "snapshot", "reset"}
			clientFuncs := []func(job string) error{c.SignalWakeup, c.SignalSnapshot, c.SignalReset}
			sigMod := tview.NewModal().AddButtons(signals)
			sigMod.SetText(fmt.Sprintf("Send a signal to job %q", job.Name()))
			showModal(sigMod, func(idx int, _ string) {
				go func() {
					if idx == -1 {
						return
					}
					err := clientFuncs[idx](job.Name())
					if err != nil {
						app.QueueUpdate(func() {
							me := tview.NewModal().SetText(fmt.Sprintf("signal error: %s", err))
							me.AddButtons([]string{"Close"})
							showModal(me, nil)
						})
					}
				}()
			})
		}

		return e
	})

	fsFilterInput.SetChangedFunc(func(searchterm string) {
		viewmodelupdate(func(p *viewmodel.Params) {
			p.FSFilter = func(fs string) bool {
				r, err := regexp.Compile(searchterm)
				if err != nil {
					return true
				}
				return r.MatchString(fs)
			}
		})
		redraw()
		jobTextDetail.ScrollToBeginning()
	})
	fsFilterInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEnter {
			app.SetFocus(jobTextDetail)
			return nil
		}
		return event
	})

	jobTextDetail.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyRune && event.Rune() == 'w' {
			// toggle wrapping
			viewmodelupdate(func(p *viewmodel.Params) {
				p.DetailViewWrap = !p.DetailViewWrap
			})
			return nil
		}
		return event
	})

	return app.Run()
}
