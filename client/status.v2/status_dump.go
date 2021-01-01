package status

import (
	"fmt"
	"os"
	"strings"

	"github.com/gdamore/tcell"
	"github.com/mattn/go-isatty"
	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/client/status.v2/viewmodel"
)

func dump(c Client) error {
	s, err := c.Status()
	if err != nil {
		return err
	}

	width := 1 << 31
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
		params.SelectedJob = j
		m.Update(params)
		fmt.Println(m.SelectedJob().FullDescription())
		fmt.Println(hline)
	}

	return nil
}
