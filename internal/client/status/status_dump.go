package status

import (
	"fmt"
	"os"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-isatty"
	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/internal/client/status/viewmodel"
)

func dump(c Client, job string) error {
	s, err := c.Status()
	if err != nil {
		return err
	}

	if job != "" {
		if _, ok := s.Jobs[job]; !ok {
			return errors.Errorf("job %q not found", job)
		}
	}

	width := (1 << 31) - 1
	wrap := false
	hline := strings.Repeat("-", 80)
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
		hline = strings.Repeat("-", width)
	}

	m := viewmodel.New()
	params := viewmodel.Params{
		Report:                  s.Jobs,
		ReportFetchError:        nil,
		SelectedJob:             nil,
		FSFilter:                func(s string) bool { return true },
		DetailViewWidth:         width,
		DetailViewWrap:          wrap,
		ShortKeybindingOverview: "",
	}
	m.Update(params)
	for _, j := range m.Jobs() {
		if job != "" && j.Name() != job {
			continue
		}
		params.SelectedJob = j
		m.Update(params)
		fmt.Println(m.SelectedJob().FullDescription())
		if job != "" {
			return nil
		} else {
			fmt.Println(hline)
		}
	}

	return nil
}
