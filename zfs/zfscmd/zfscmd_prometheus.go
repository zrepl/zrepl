package zfscmd

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var metrics struct {
	totaltime  *prometheus.HistogramVec
	systemtime *prometheus.HistogramVec
	usertime   *prometheus.HistogramVec
}

var timeLabels = []string{"jobid", "zfsbinary", "zfsverb"}
var timeBuckets = []float64{0.01, 0.1, 0.2, 0.5, 0.75, 1, 2, 5, 10, 60}

func init() {
	metrics.totaltime = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "zrepl",
		Subsystem: "zfscmd",
		Name:      "runtime",
		Help:      "number of seconds that the command took from start until wait returned",
		Buckets:   timeBuckets,
	}, timeLabels)
	metrics.systemtime = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "zrepl",
		Subsystem: "zfscmd",
		Name:      "systemtime",
		Help:      "https://golang.org/pkg/os/#ProcessState.SystemTime",
		Buckets:   timeBuckets,
	}, timeLabels)
	metrics.usertime = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "zrepl",
		Subsystem: "zfscmd",
		Name:      "usertime",
		Help:      "https://golang.org/pkg/os/#ProcessState.UserTime",
		Buckets:   timeBuckets,
	}, timeLabels)

}

func RegisterMetrics(r prometheus.Registerer) {
	r.MustRegister(metrics.totaltime)
	r.MustRegister(metrics.systemtime)
	r.MustRegister(metrics.usertime)
}

func waitPostPrometheus(c *Cmd, err error, now time.Time) {

	if len(c.cmd.Args) < 2 {
		getLogger(c.ctx).WithField("args", c.cmd.Args).
			Warn("prometheus: cannot turn zfs command into metric")
		return
	}

	// Note: do not start parsing other aspects
	// of the ZFS command line. This is not the suitable layer
	// for such a task.

	jobid := getJobIDOrDefault(c.ctx, "_nojobid")

	labelValues := []string{jobid, c.cmd.Args[0], c.cmd.Args[1]}

	metrics.totaltime.
		WithLabelValues(labelValues...).
		Observe(c.Runtime().Seconds())
	metrics.systemtime.WithLabelValues(labelValues...).
		Observe(c.cmd.ProcessState.SystemTime().Seconds())
	metrics.usertime.WithLabelValues(labelValues...).
		Observe(c.cmd.ProcessState.UserTime().Seconds())

}
