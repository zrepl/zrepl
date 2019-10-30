package daemon

import (
	"context"
	"net"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/job"
	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/rpc/dataconn/frameconn"
	"github.com/zrepl/zrepl/zfs"
)

type prometheusJob struct {
	listen string
}

func newPrometheusJobFromConfig(in *config.PrometheusMonitoring) (*prometheusJob, error) {
	if _, _, err := net.SplitHostPort(in.Listen); err != nil {
		return nil, err
	}
	return &prometheusJob{in.Listen}, nil
}

var prom struct {
	taskLogEntries *prometheus.CounterVec
}

func init() {
	prom.taskLogEntries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "zrepl",
		Subsystem: "daemon",
		Name:      "log_entries",
		Help:      "number of log entries per job task and level",
	}, []string{"zrepl_job", "level"})
	prometheus.MustRegister(prom.taskLogEntries)
}

func (j *prometheusJob) Name() string { return jobNamePrometheus }

func (j *prometheusJob) Status() *job.Status { return &job.Status{Type: job.TypeInternal} }

func (j *prometheusJob) OwnedDatasetSubtreeRoot() (p *zfs.DatasetPath, ok bool) { return nil, false }

func (j *prometheusJob) RegisterMetrics(registerer prometheus.Registerer) {}

func (j *prometheusJob) Run(ctx context.Context) {

	if err := zfs.PrometheusRegister(prometheus.DefaultRegisterer); err != nil {
		panic(err)
	}

	if err := frameconn.PrometheusRegister(prometheus.DefaultRegisterer); err != nil {
		panic(err)
	}

	log := job.GetLogger(ctx)

	l, err := net.Listen("tcp", j.listen)
	if err != nil {
		log.WithError(err).Error("cannot listen")
		return
	}
	go func() {
		<-ctx.Done()
		l.Close()
	}()

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	err = http.Serve(l, mux)
	if err != nil && ctx.Err() == nil {
		log.WithError(err).Error("error while serving")
	}

}

type prometheusJobOutlet struct {
	jobName string
}

var _ logger.Outlet = prometheusJobOutlet{}

func newPrometheusLogOutlet(jobName string) prometheusJobOutlet {
	return prometheusJobOutlet{jobName}
}

func (o prometheusJobOutlet) WriteEntry(entry logger.Entry) error {
	prom.taskLogEntries.WithLabelValues(o.jobName, entry.Level.String()).Inc()
	return nil
}
