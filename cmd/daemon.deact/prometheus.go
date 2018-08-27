package daemon

import (
	"context"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/zrepl/zrepl/cmd/daemon/job"
	"github.com/zrepl/zrepl/zfs"
	"net"
	"net/http"
)

type prometheusJob struct {
	listen string
}

func newPrometheusJob(listen string) *prometheusJob {
	return &prometheusJob{listen}
}

var prom struct {
	taskLastActiveStart    *prometheus.GaugeVec
	taskLastActiveDuration *prometheus.GaugeVec
	taskLogEntries         *prometheus.CounterVec
}

func init() {
	prom.taskLastActiveStart = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "zrepl",
		Subsystem: "daemon",
		Name:      "task_last_active_start",
		Help:      "point in time at which the job task last left idle state",
	}, []string{"zrepl_job", "job_type", "task"})
	prom.taskLastActiveDuration = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "zrepl",
		Subsystem: "daemon",
		Name:      "task_last_active_duration",
		Help:      "seconds that the last run ob a job task spent between leaving and re-entering idle state",
	}, []string{"zrepl_job", "job_type", "task"})
	prom.taskLogEntries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "zrepl",
		Subsystem: "daemon",
		Name:      "task_log_entries",
		Help:      "number of log entries per job task and level",
	}, []string{"zrepl_job", "job_type", "task", "level"})
	prometheus.MustRegister(prom.taskLastActiveStart)
	prometheus.MustRegister(prom.taskLastActiveDuration)
	prometheus.MustRegister(prom.taskLogEntries)
}

func (j *prometheusJob) Name() string { return jobNamePrometheus }

func (j *prometheusJob) Status() interface{} { return nil }

func (j *prometheusJob) Run(ctx context.Context) {

	if err := zfs.PrometheusRegister(prometheus.DefaultRegisterer); err != nil {
		panic(err)
	}

	log := job.GetLogger(ctx)

	l, err := net.Listen("tcp", j.listen)
	if err != nil {
		log.WithError(err).Error("cannot listen")
	}
	go func() {
		select {
		case <-ctx.Done():
			l.Close()
		}
	}()

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	err = http.Serve(l, mux)
	if err != nil {
		log.WithError(err).Error("error while serving")
	}

}
