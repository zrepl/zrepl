package daemon

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/zrepl/zrepl/daemon/logging/trace"
	"github.com/zrepl/zrepl/endpoint"
	"github.com/zrepl/zrepl/util/envconst"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/job"
	"github.com/zrepl/zrepl/daemon/job/reset"
	"github.com/zrepl/zrepl/daemon/job/wakeup"
	"github.com/zrepl/zrepl/daemon/logging"
	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/version"
	"github.com/zrepl/zrepl/zfs/zfscmd"
)

func Run(ctx context.Context, conf *config.Config) error {
	ctx, cancel := context.WithCancel(ctx)

	defer cancel()
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	outlets, err := logging.OutletsFromConfig(*conf.Global.Logging)
	if err != nil {
		return errors.Wrap(err, "cannot build logging from config")
	}
	outlets.Add(newPrometheusLogOutlet(), logger.Debug)

	confJobs, err := job.JobsFromConfig(conf)
	if err != nil {
		return errors.Wrap(err, "cannot build jobs from config")
	}

	log := logger.NewLogger(outlets, 1*time.Second)
	log.Info(version.NewZreplVersionInformation().String())

	ctx = logging.WithLoggers(ctx, logging.SubsystemLoggersWithUniversalLogger(log))
	trace.RegisterCallback(trace.Callback{
		OnBegin: func(ctx context.Context) { logging.GetLogger(ctx, logging.SubsysTraceData).Debug("begin span") },
		OnEnd: func(ctx context.Context, spanInfo trace.SpanInfo) {
			logging.
				GetLogger(ctx, logging.SubsysTraceData).
				WithField("duration_s", spanInfo.EndedAt().Sub(spanInfo.StartedAt()).Seconds()).
				Debug("finished span " + spanInfo.TaskAndSpanStack(trace.SpanStackKindAnnotation))
		},
	})

	for _, job := range confJobs {
		if IsInternalJobName(job.Name()) {
			panic(fmt.Sprintf("internal job name used for config job '%s'", job.Name())) //FIXME
		}
	}

	jobs := newJobs()

	// start control socket
	controlJob, err := newControlJob(conf.Global.Control.SockPath, jobs)
	if err != nil {
		panic(err) // FIXME
	}
	jobs.start(ctx, controlJob, true)

	for i, jc := range conf.Global.Monitoring {
		var (
			job job.Job
			err error
		)
		switch v := jc.Ret.(type) {
		case *config.PrometheusMonitoring:
			job, err = newPrometheusJobFromConfig(v)
		default:
			return errors.Errorf("unknown monitoring job #%d (type %T)", i, v)
		}
		if err != nil {
			return errors.Wrapf(err, "cannot build monitoring job #%d", i)
		}
		jobs.start(ctx, job, true)
	}

	// register global (=non job-local) metrics
	version.PrometheusRegister(prometheus.DefaultRegisterer)
	zfscmd.RegisterMetrics(prometheus.DefaultRegisterer)
	trace.RegisterMetrics(prometheus.DefaultRegisterer)
	endpoint.RegisterMetrics(prometheus.DefaultRegisterer)

	log.Info("starting daemon")

	// start regular jobs
	for _, j := range confJobs {
		jobs.start(ctx, j, false)
	}

	select {
	case <-jobs.wait():
		log.Info("all jobs finished")
	case <-ctx.Done():
		log.WithError(ctx.Err()).Info("context finished")
	}
	log.Info("waiting for jobs to finish")
	<-jobs.wait()
	log.Info("daemon exiting")
	return nil
}

type jobs struct {
	wg sync.WaitGroup

	// m protects all fields below it
	m       sync.RWMutex
	wakeups map[string]wakeup.Func // by Job.Name
	resets  map[string]reset.Func  // by Job.Name
	jobs    map[string]job.Job
}

func newJobs() *jobs {
	return &jobs{
		wakeups: make(map[string]wakeup.Func),
		resets:  make(map[string]reset.Func),
		jobs:    make(map[string]job.Job),
	}
}

func (s *jobs) wait() <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(ch)
	}()
	return ch
}

type Status struct {
	Jobs   map[string]*job.Status
	Global GlobalStatus
}

type GlobalStatus struct {
	ZFSCmds  *zfscmd.Report
	Envconst *envconst.Report
}

func (s *jobs) status() map[string]*job.Status {
	s.m.RLock()
	defer s.m.RUnlock()

	type res struct {
		name   string
		status *job.Status
	}
	var wg sync.WaitGroup
	c := make(chan res, len(s.jobs))
	for name, j := range s.jobs {
		wg.Add(1)
		go func(name string, j job.Job) {
			defer wg.Done()
			c <- res{name: name, status: j.Status()}
		}(name, j)
	}
	wg.Wait()
	close(c)
	ret := make(map[string]*job.Status, len(s.jobs))
	for res := range c {
		ret[res.name] = res.status
	}
	return ret
}

func (s *jobs) wakeup(job string) error {
	s.m.RLock()
	defer s.m.RUnlock()

	wu, ok := s.wakeups[job]
	if !ok {
		return errors.Errorf("Job %s does not exist", job)
	}
	return wu()
}

func (s *jobs) reset(job string) error {
	s.m.RLock()
	defer s.m.RUnlock()

	wu, ok := s.resets[job]
	if !ok {
		return errors.Errorf("Job %s does not exist", job)
	}
	return wu()
}

const (
	jobNamePrometheus = "_prometheus"
	jobNameControl    = "_control"
)

func IsInternalJobName(s string) bool {
	return strings.HasPrefix(s, "_")
}

func (s *jobs) start(ctx context.Context, j job.Job, internal bool) {
	s.m.Lock()
	defer s.m.Unlock()

	ctx = logging.WithInjectedField(ctx, logging.JobField, j.Name())

	jobName := j.Name()
	if !internal && IsInternalJobName(jobName) {
		panic(fmt.Sprintf("internal job name used for non-internal job %s", jobName))
	}
	if internal && !IsInternalJobName(jobName) {
		panic(fmt.Sprintf("internal job does not use internal job name %s", jobName))
	}
	if _, ok := s.jobs[jobName]; ok {
		panic(fmt.Sprintf("duplicate job name %s", jobName))
	}

	j.RegisterMetrics(prometheus.DefaultRegisterer)

	s.jobs[jobName] = j
	ctx = zfscmd.WithJobID(ctx, j.Name())
	ctx, wakeup := wakeup.Context(ctx)
	ctx, resetFunc := reset.Context(ctx)
	s.wakeups[jobName] = wakeup
	s.resets[jobName] = resetFunc

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		job.GetLogger(ctx).Info("starting job")
		defer job.GetLogger(ctx).Info("job exited")
		j.Run(ctx)
	}()
}
