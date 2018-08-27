package daemon

import (
	"context"
	"fmt"
	"github.com/zrepl/zrepl/daemon/job"
	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/version"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
	"github.com/zrepl/zrepl/cmd/config"
	"github.com/zrepl/zrepl/daemon/logging"
	"github.com/pkg/errors"
)


func Run(conf config.Config) error {

	ctx, cancel := context.WithCancel(context.Background())

	defer cancel()
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	outlets, err := logging.OutletsFromConfig(conf.Global.Logging)
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

	// start prometheus
	//var promJob *prometheusJob // FIXME
	//jobs.start(ctx, promJob, true)

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
	wakeups map[string]job.WakeupChan // by JobName
	jobs    map[string]job.Job
}

func newJobs() *jobs {
	return &jobs{
		wakeups: make(map[string]job.WakeupChan),
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

func (s *jobs) status() map[string]interface{} {
	s.m.RLock()
	defer s.m.RUnlock()

	type res struct {
		name   string
		status interface{}
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
	ret := make(map[string]interface{}, len(s.jobs))
	for res := range c {
		ret[res.name] = res.status
	}
	return ret
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

	jobLog := job.GetLogger(ctx).WithField(logJobField, j.Name())
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
	s.jobs[jobName] = j
	ctx = job.WithLogger(ctx, jobLog)
	ctx, wakeupChan := job.WithWakeup(ctx)
	s.wakeups[jobName] = wakeupChan

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		jobLog.Info("starting job")
		defer jobLog.Info("job exited")
		j.Run(ctx)
	}()
}
