package jobrun

import (
	"fmt"
	"os"
	"time"
)

type Job struct {
	Name      string
	RunFunc   func() (err error)
	LastStart time.Time
	LastError error
	Interval  time.Duration
	Repeats   bool
}

type JobRunner struct {
	notificationChan chan Job
	newJobChan       chan Job
	finishedJobChan  chan Job
	scheduleTimer    <-chan time.Time
	pending          map[string]Job
	running          map[string]Job
}

func NewJobRunner() *JobRunner {
	return &JobRunner{
		notificationChan: make(chan Job),
		newJobChan:       make(chan Job),
		finishedJobChan:  make(chan Job),
		pending:          make(map[string]Job),
		running:          make(map[string]Job),
	}
}

func (r *JobRunner) AddJobChan() chan<- Job {
	return r.newJobChan
}

func (r *JobRunner) AddJob(j Job) {
	r.newJobChan <- j
}

func (r *JobRunner) NotificationChan() <-chan Job {
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

		runTime := time.Since(finishedJob.LastStart)

		fmt.Fprintf(os.Stderr, "[%s] finished after %v\n", finishedJob.Name, runTime)
		if runTime > finishedJob.Interval {
			fmt.Fprintf(os.Stderr, "[%s] WARN: job exceeded interval of %v\n", finishedJob.Name, finishedJob.Interval)
		}

		delete(r.running, finishedJob.Name)
		if finishedJob.Repeats {
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

		jobDueTime := job.LastStart.Add(job.Interval)

		if jobDueTime.After(time.Now()) {
			if jobDueTime.Before(nextJobDue) {
				nextJobDue = jobDueTime
			}
			jobPending = true
			continue
		}
		// This job is due, run it

		delete(r.pending, jobName)
		r.running[jobName] = job
		job.LastStart = now

		go func(job Job) {
			if err := job.RunFunc(); err != nil {
				job.LastError = err
				r.notificationChan <- job
			}
			r.finishedJobChan <- job
		}(job)

	}

	if jobPending || len(r.running) > 0 {
		nextJobDue = nextJobDue.Add(time.Second).Round(time.Second)
		fmt.Fprintf(os.Stderr, "jobrun: waiting until %v\n", nextJobDue)
		r.scheduleTimer = time.After(nextJobDue.Sub(now))
		goto loop
	}

}
