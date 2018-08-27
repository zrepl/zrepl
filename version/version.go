package version

import (
	"fmt"
	"runtime"
)

var (
	zreplVersion string // set by build infrastructure
)

type ZreplVersionInformation struct {
	Version         string
	RuntimeGOOS     string
	RuntimeGOARCH   string
	RUNTIMECompiler string
}

func NewZreplVersionInformation() *ZreplVersionInformation {
	return &ZreplVersionInformation{
		Version:         zreplVersion,
		RuntimeGOOS:     runtime.GOOS,
		RuntimeGOARCH:   runtime.GOARCH,
		RUNTIMECompiler: runtime.Compiler,
	}
}

func (i *ZreplVersionInformation) String() string {
	return fmt.Sprintf("zrepl version=%s GOOS=%s GOARCH=%s Compiler=%s",
		i.Version, i.RuntimeGOOS, i.RuntimeGOARCH, i.RUNTIMECompiler)
}
