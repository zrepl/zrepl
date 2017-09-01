package jobrun

import (
	"fmt"
	"sync"
	"time"
)

type Logger interface {
	Printf(format string, v ...interface{})
}

type jobLogger struct {
	MainLog Logger
	JobName string
}

func (l jobLogger) Printf(format string, v ...interface{}) {
	l.MainLog.Printf(fmt.Sprintf("[%s]: %s", l.JobName, format), v...)
}

type Job interface {
	JobName() string
	JobDo(log Logger) (err error)
	JobRepeatStrategy() RepeatStrategy
}

type JobEvent interface{}

type JobFinishedEvent struct {
	Job    Job
	Result JobRunResult
}

type JobScheduledEvent struct {
	Job   Job
	DueAt time.Time
}

type JobrunIdleEvent struct {
	SleepUntil time.Time
}

type JobrunFinishedEvent struct{}

type JobMetadata struct {
	Job       Job
	name      string
	LastStart time.Time
	LastError error
	DueAt     time.Time
}

type JobRunResult struct {
	Start  time.Time
	Finish time.Time
	Error  error
}

func (r JobRunResult) RunTime() time.Duration { return r.Finish.Sub(r.Start) }

type RepeatStrategy interface {
	ShouldReschedule(lastResult JobRunResult) (nextDue time.Time, reschedule bool)
}

type JobRunner struct {
	logger           Logger
	notificationChan chan<- JobEvent
	newJobChan       chan Job
	finishedJobChan  chan JobMetadata
	scheduleTimer    <-chan time.Time
	pending          map[string]JobMetadata
	running          map[string]JobMetadata
	wait             sync.WaitGroup
}

func NewJobRunner(logger Logger) *JobRunner {
	return &JobRunner{
		logger:          logger,
		newJobChan:      make(chan Job),
		finishedJobChan: make(chan JobMetadata),
		pending:         make(map[string]JobMetadata),
		running:         make(map[string]JobMetadata),
	}
}

func (r *JobRunner) AddJob(j Job) {
	go func(j Job) {
		r.newJobChan <- j
	}(j)
}

func (r *JobRunner) SetNotificationChannel(c chan<- JobEvent) {
	r.notificationChan = c
}

func (r *JobRunner) postEvent(n JobEvent) {
	if r.notificationChan != nil {
		r.notificationChan <- n
	}
}

func (r *JobRunner) Run() {

loop:
	select {

	case j := <-r.newJobChan:

		jn := j.JobName()

		_, jobPending := r.pending[jn]
		_, jobRunning := r.running[jn]

		if jobPending || jobRunning {
			panic("job already in runner")
		}

		jm := JobMetadata{name: jn, Job: j}

		r.pending[jn] = jm

	case finishedJob := <-r.finishedJobChan:

		delete(r.running, finishedJob.name)

		res := JobRunResult{
			Start:  finishedJob.LastStart,
			Finish: time.Now(),
			Error:  finishedJob.LastError,
		}

		r.postEvent(JobFinishedEvent{finishedJob.Job, res})

		dueTime, resched := finishedJob.Job.JobRepeatStrategy().ShouldReschedule(res)
		if resched {
			r.postEvent(JobScheduledEvent{finishedJob.Job, dueTime})
			finishedJob.DueAt = dueTime
			r.pending[finishedJob.name] = finishedJob
		}

	case <-r.scheduleTimer:
	}

	if len(r.pending) == 0 && len(r.running) == 0 {
		return
	}

	// Find jobs to run
	var now time.Time
	var jobPending bool

	now = time.Now()
	jobPending = false

	nextJobDue := now.Add(time.Minute) // max(pending.Interval)

	for jobName, job := range r.pending {

		if job.DueAt.After(now) {
			if job.DueAt.Before(nextJobDue) {
				nextJobDue = job.DueAt
			}
			jobPending = true
			continue
		}
		// This job is due, run it

		delete(r.pending, jobName)
		r.running[jobName] = job
		job.LastStart = now

		go func(job JobMetadata) {
			jobLog := jobLogger{r.logger, job.name}
			job.LastError = job.Job.JobDo(jobLog)
			r.finishedJobChan <- job
		}(job)

	}

	if jobPending || len(r.running) > 0 {
		r.postEvent(JobrunIdleEvent{nextJobDue})
		r.scheduleTimer = time.After(nextJobDue.Sub(now))
		goto loop
	}

	r.postEvent(JobrunFinishedEvent{})

}
