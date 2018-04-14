package cmd

import (
	"context"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/zrepl/zrepl/zfs"
	"net"
	"net/http"
)

type PrometheusJob struct {
	Name   string
	Listen string
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

func parsePrometheusJob(c JobParsingContext, name string, i map[string]interface{}) (j *PrometheusJob, err error) {
	var s struct {
		Listen string
	}
	if err := mapstructure.Decode(i, &s); err != nil {
		return nil, errors.Wrap(err, "mapstructure error")
	}
	if s.Listen == "" {
		return nil, errors.New("must specify 'listen' attribute")
	}
	return &PrometheusJob{name, s.Listen}, nil
}

func (j *PrometheusJob) JobName() string { return j.Name }

func (j *PrometheusJob) JobType() JobType { return JobTypePrometheus }

func (j *PrometheusJob) JobStart(ctx context.Context) {

	if err := zfs.PrometheusRegister(prometheus.DefaultRegisterer); err != nil {
		panic(err)
	}

	log := ctx.Value(contextKeyLog).(Logger)
	task := NewTask("main", j, log)
	log = task.Log()

	l, err := net.Listen("tcp", j.Listen)
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

func (*PrometheusJob) JobStatus(ctxt context.Context) (*JobStatus, error) {
	return &JobStatus{}, nil
}
