package version

import (
	"fmt"
	"runtime"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	zreplVersion string // set by build infrastructure
)

type ZreplVersionInformation struct {
	Version         string
	RuntimeGo       string
	RuntimeGOOS     string
	RuntimeGOARCH   string
	RUNTIMECompiler string
}

func NewZreplVersionInformation() *ZreplVersionInformation {
	return &ZreplVersionInformation{
		Version:         zreplVersion,
		RuntimeGo:       runtime.Version(),
		RuntimeGOOS:     runtime.GOOS,
		RuntimeGOARCH:   runtime.GOARCH,
		RUNTIMECompiler: runtime.Compiler,
	}
}

func (i *ZreplVersionInformation) String() string {
	return fmt.Sprintf("zrepl version=%s go=%s GOOS=%s GOARCH=%s Compiler=%s",
		i.Version, i.RuntimeGo, i.RuntimeGOOS, i.RuntimeGOARCH, i.RUNTIMECompiler)
}

var prometheusMetric = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Namespace: "zrepl",
		Name:      "start_time",
		Help:      "zrepl daemon start time and version",
		ConstLabels: map[string]string{
			"raw":          zreplVersion,
			"version_info": NewZreplVersionInformation().String(),
		},
	},
)

func PrometheusRegister(r prometheus.Registerer) {
	r.MustRegister(prometheusMetric)
	prometheusMetric.SetToCurrentTime()
}
