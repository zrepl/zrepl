package jobrun

import (
	"fmt"
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
	l.MainLog.Printf(fmt.Sprintf("job[%s]: %s", l.JobName, format), v...)
}

type JobMetadata struct {
	Name           string
	RunFunc        func(log Logger) (err error)
	LastStart      time.Time
	LastError      error
	DueAt          time.Time
	RepeatStrategy RepeatStrategy
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
	notificationChan chan JobMetadata
	newJobChan       chan JobMetadata
	finishedJobChan  chan JobMetadata
	scheduleTimer    <-chan time.Time
	pending          map[string]JobMetadata
	running          map[string]JobMetadata
}

func NewJobRunner(logger Logger) *JobRunner {
	return &JobRunner{
		logger:           logger,
		notificationChan: make(chan JobMetadata),
		newJobChan:       make(chan JobMetadata),
		finishedJobChan:  make(chan JobMetadata),
		pending:          make(map[string]JobMetadata),
		running:          make(map[string]JobMetadata),
	}
}

func (r *JobRunner) AddJobChan() chan<- JobMetadata {
	return r.newJobChan
}

func (r *JobRunner) AddJob(j JobMetadata) {
	r.newJobChan <- j
}

func (r *JobRunner) NotificationChan() <-chan JobMetadata {
	return r.notificationChan
}

func (r *JobRunner) Start() {

loop:
	select {

	case newJob := <-r.newJobChan:

		_, jobPending := r.pending[newJob.Name]
		_, jobRunning := r.running[newJob.Name]

		if jobPending || jobRunning {
			panic("job already in runner")
		}

		r.pending[newJob.Name] = newJob

	case finishedJob := <-r.finishedJobChan:

		delete(r.running, finishedJob.Name)

		res := JobRunResult{
			Start:  finishedJob.LastStart,
			Finish: time.Now(),
			Error:  finishedJob.LastError,
		}

		r.logger.Printf("[%s] finished after %s\n", finishedJob.Name, res.RunTime())

		dueTime, resched := finishedJob.RepeatStrategy.ShouldReschedule(res)
		if resched {
			r.logger.Printf("[%s] rescheduling to %s", finishedJob.Name, dueTime)
			finishedJob.DueAt = dueTime
			r.pending[finishedJob.Name] = finishedJob
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
			jobLog := jobLogger{r.logger, job.Name}
			if err := job.RunFunc(jobLog); err != nil {
				job.LastError = err
				r.notificationChan <- job
			}
			r.finishedJobChan <- job
		}(job)

	}

	if jobPending || len(r.running) > 0 {
		r.logger.Printf("waiting until %v\n", nextJobDue)
		r.scheduleTimer = time.After(nextJobDue.Sub(now))
		goto loop
	}

}
