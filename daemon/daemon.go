package daemon

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/job"
	"github.com/zrepl/zrepl/daemon/job/wakeup"
	"github.com/zrepl/zrepl/daemon/logging"
	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/version"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

func Run(conf *config.Config) error {

	ctx, cancel := context.WithCancel(context.Background())

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

	confJobs, err := job.JobsFromConfig(conf)
	if err != nil {
		return errors.Wrap(err, "cannot build jobs from config")
	}

	log := logger.NewLogger(outlets, 1*time.Second)
	log.Info(version.NewZreplVersionInformation().String())

	for _, job := range confJobs {
		if IsInternalJobName(job.Name()) {
			panic(fmt.Sprintf("internal job name used for config job '%s'", job.Name())) //FIXME
		}
	}

	ctx = job.WithLogger(ctx, log)

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
			return errors.Wrapf(err,"cannot build monitorin gjob #%d", i)
		}
		jobs.start(ctx, job, true)
	}


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
	log.Info("daemon exiting")
	return nil
}

type jobs struct {
	wg sync.WaitGroup

	// m protects all fields below it
	m       sync.RWMutex
	wakeups map[string]wakeup.Func // by Job.Name
	jobs    map[string]job.Job
}

func newJobs() *jobs {
	return &jobs{
		wakeups: make(map[string]wakeup.Func),
		jobs:    make(map[string]job.Job),
	}
}

const (
	logJobField    string = "job"
	logTaskField   string = "task"
	logSubsysField string = "subsystem"
)

func (s *jobs) wait() <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		s.wg.Wait()
	}()
	return ch
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

	jobLog := job.GetLogger(ctx).
		WithField(logJobField, j.Name()).
		WithOutlet(newPrometheusLogOutlet(j.Name()), logger.Debug)
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
	ctx = job.WithLogger(ctx, jobLog)
	ctx, wakeup := wakeup.Context(ctx)
	s.wakeups[jobName] = wakeup

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		jobLog.Info("starting job")
		defer jobLog.Info("job exited")
		j.Run(ctx)
	}()
}
