package cmd

import (
	"container/list"
	"context"
	"fmt"
	"github.com/spf13/cobra"
	"github.com/zrepl/zrepl/logger"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

// daemonCmd represents the daemon command
var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "start daemon",
	Run:   doDaemon,
}

func init() {
	RootCmd.AddCommand(daemonCmd)
}

type Job interface {
	JobName() string
	JobType() JobType
	JobStart(ctxt context.Context)
	JobStatus(ctxt context.Context) (*JobStatus, error)
}

type JobType string

const (
	JobTypePull       JobType = "pull"
	JobTypeSource     JobType = "source"
	JobTypeLocal      JobType = "local"
	JobTypePrometheus JobType = "prometheus"
	JobTypeControl    JobType = "control"
)

func ParseUserJobType(s string) (JobType, error) {
	switch s {
	case "pull":
		return JobTypePull, nil
	case "source":
		return JobTypeSource, nil
	case "local":
		return JobTypeLocal, nil
	case "prometheus":
		return JobTypePrometheus, nil
	}
	return "", fmt.Errorf("unknown job type '%s'", s)
}

func (j JobType) String() string {
	return string(j)
}

func doDaemon(cmd *cobra.Command, args []string) {

	conf, err := ParseConfig(rootArgs.configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error parsing config: %s\n", err)
		os.Exit(1)
	}

	log := logger.NewLogger(conf.Global.logging.Outlets, 1*time.Second)

	log.Info(NewZreplVersionInformation().String())
	log.Debug("starting daemon")
	ctx := context.WithValue(context.Background(), contextKeyLog, log)
	ctx = context.WithValue(ctx, contextKeyLog, log)

	d := NewDaemon(conf)
	d.Loop(ctx)

}

type contextKey string

const (
	contextKeyLog    contextKey = contextKey("log")
	contextKeyDaemon contextKey = contextKey("daemon")
)

type Daemon struct {
	conf      *Config
	startedAt time.Time
}

func NewDaemon(initialConf *Config) *Daemon {
	return &Daemon{conf: initialConf}
}

func (d *Daemon) Loop(ctx context.Context) {

	d.startedAt = time.Now()

	log := ctx.Value(contextKeyLog).(Logger)

	ctx, cancel := context.WithCancel(ctx)
	ctx = context.WithValue(ctx, contextKeyDaemon, d)

	sigChan := make(chan os.Signal, 1)
	finishs := make(chan Job)

	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	log.Info("starting jobs from config")
	i := 0
	for _, job := range d.conf.Jobs {
		logger := log.WithField(logJobField, job.JobName())
		logger.Info("starting")
		i++
		jobCtx := context.WithValue(ctx, contextKeyLog, logger)
		go func(j Job) {
			j.JobStart(jobCtx)
			finishs <- j
		}(job)
	}

	finishCount := 0
outer:
	for {
		select {
		case <-finishs:
			finishCount++
			if finishCount == len(d.conf.Jobs) {
				log.Info("all jobs finished")
				break outer
			}

		case sig := <-sigChan:
			log.WithField("signal", sig).Info("received signal")
			log.Info("cancelling all jobs")
			cancel()
		}
	}

	signal.Stop(sigChan)
	cancel() // make go vet happy

	log.Info("exiting")

}

// Representation of a Job's status that is composed of Tasks
type JobStatus struct {
	// Statuses of all tasks of this job
	Tasks []*TaskStatus
	// Error != "" if JobStatus() returned an error
	JobStatusError string
}

// Representation of a Daemon's status that is composed of Jobs
type DaemonStatus struct {
	StartedAt time.Time
	Jobs      map[string]*JobStatus
}

func (d *Daemon) Status() (s *DaemonStatus) {

	s = &DaemonStatus{}
	s.StartedAt = d.startedAt

	s.Jobs = make(map[string]*JobStatus, len(d.conf.Jobs))

	for name, j := range d.conf.Jobs {
		status, err := j.JobStatus(context.TODO())
		if err != nil {
			s.Jobs[name] = &JobStatus{nil, err.Error()}
			continue
		}
		s.Jobs[name] = status
	}

	return
}

// Representation of a Task's status
type TaskStatus struct {
	Name string
	// Whether the task is idle.
	Idle bool
	// The stack of activities the task is currently executing.
	// The first element is the root activity and equal to Name.
	ActivityStack []string
	// Number of bytes received by the task since it last left idle state.
	ProgressRx int64
	// Number of bytes sent by the task since it last left idle state.
	ProgressTx int64
	// Log entries emitted by the task since it last left idle state.
	// Only contains the log entries emitted through the task's logger
	// (provided by Task.Log()).
	LogEntries []logger.Entry
	// The maximum log level of LogEntries.
	// Only valid if len(LogEntries) > 0.
	MaxLogLevel logger.Level
	// Last time something about the Task changed
	LastUpdate time.Time
}

// An instance of Task tracks  a single thread of activity that is part of a Job.
type Task struct {
	name   string // immutable
	parent Job    // immutable

	// Stack of activities the task is currently in
	// Members are instances of taskActivity
	activities *list.List
	// Last time activities was changed (not the activities inside, the list)
	activitiesLastUpdate time.Time
	// Protects Task members from modification
	rwl sync.RWMutex
}

// Structure that describes the progress a Task has made
type taskProgress struct {
	rx         int64
	tx         int64
	creation   time.Time
	lastUpdate time.Time
	logEntries []logger.Entry
	mtx        sync.RWMutex
}

func newTaskProgress() (p *taskProgress) {
	return &taskProgress{
		creation:   time.Now(),
		logEntries: make([]logger.Entry, 0),
	}
}

func (p *taskProgress) UpdateIO(drx, dtx int64) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.rx += drx
	p.tx += dtx
	p.lastUpdate = time.Now()
}

func (p *taskProgress) UpdateLogEntry(entry logger.Entry) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	// FIXME: ensure maximum size (issue #48)
	p.logEntries = append(p.logEntries, entry)
	p.lastUpdate = time.Now()
}

func (p *taskProgress) DeepCopy() (out taskProgress) {
	p.mtx.RLock()
	defer p.mtx.RUnlock()
	out.rx, out.tx = p.rx, p.tx
	out.creation = p.creation
	out.lastUpdate = p.lastUpdate
	out.logEntries = make([]logger.Entry, len(p.logEntries))
	for i := range p.logEntries {
		out.logEntries[i] = p.logEntries[i]
	}
	return
}

// returns a copy of this taskProgress, the mutex carries no semantic value
func (p *taskProgress) Read() (out taskProgress) {
	p.mtx.RLock()
	defer p.mtx.RUnlock()
	return p.DeepCopy()
}

// Element of a Task's activity stack
type taskActivity struct {
	name   string
	idle   bool
	logger *logger.Logger
	// The progress of the task that is updated by UpdateIO() and UpdateLogEntry()
	//
	// Progress happens on a task-level and is thus global to the task.
	// That's why progress is just a pointer to the current taskProgress:
	// we reset progress when leaving the idle root activity
	progress *taskProgress
}

func NewTask(name string, parent Job, lg *logger.Logger) *Task {
	t := &Task{
		name:       name,
		parent:     parent,
		activities: list.New(),
	}
	rootLogger := lg.ReplaceField(logTaskField, name).
		WithOutlet(t, logger.Debug)
	rootAct := &taskActivity{name, true, rootLogger, newTaskProgress()}
	t.activities.PushFront(rootAct)
	return t
}

// callers must hold t.rwl
func (t *Task) cur() *taskActivity {
	return t.activities.Front().Value.(*taskActivity)
}

// buildActivityStack returns the stack of activity names
// t.rwl must be held, but the slice can be returned since strings are immutable
func (t *Task) buildActivityStack() []string {
	comps := make([]string, 0, t.activities.Len())
	for e := t.activities.Back(); e != nil; e = e.Prev() {
		act := e.Value.(*taskActivity)
		comps = append(comps, act.name)
	}
	return comps
}

// Start a sub-activity.
// Must always be matched with a call to t.Finish()
// --- consider using defer for this purpose.
func (t *Task) Enter(activity string) {
	t.rwl.Lock()
	defer t.rwl.Unlock()

	prev := t.cur()
	if prev.idle {
		// reset progress when leaving idle task
		// we leave the old progress dangling to have the user not worry about
		prev.progress = newTaskProgress()

		prom.taskLastActiveStart.WithLabelValues(
			t.parent.JobName(),
			t.parent.JobType().String(),
			t.name).
			Set(float64(prev.progress.creation.UnixNano()) / 1e9)

	}
	act := &taskActivity{activity, false, nil, prev.progress}
	t.activities.PushFront(act)
	stack := t.buildActivityStack()
	activityField := strings.Join(stack, ".")
	act.logger = prev.logger.ReplaceField(logTaskField, activityField)

	t.activitiesLastUpdate = time.Now()
}

func (t *Task) UpdateProgress(dtx, drx int64) {
	t.rwl.RLock()
	p := t.cur().progress // protected by own rwlock
	t.rwl.RUnlock()
	p.UpdateIO(dtx, drx)
}

// Returns a wrapper io.Reader that updates this task's _current_ progress value.
// Progress updates after this task resets its progress value are discarded.
func (t *Task) ProgressUpdater(r io.Reader) *IOProgressUpdater {
	t.rwl.RLock()
	defer t.rwl.RUnlock()
	return &IOProgressUpdater{r, t.cur().progress}
}

func (t *Task) Status() *TaskStatus {
	t.rwl.RLock()
	defer t.rwl.RUnlock()
	// NOTE
	// do not return any state in TaskStatus that is protected by t.rwl

	cur := t.cur()
	stack := t.buildActivityStack()
	prog := cur.progress.Read()

	var maxLevel logger.Level
	for _, entry := range prog.logEntries {
		if maxLevel < entry.Level {
			maxLevel = entry.Level
		}
	}

	lastUpdate := prog.lastUpdate
	if lastUpdate.Before(t.activitiesLastUpdate) {
		lastUpdate = t.activitiesLastUpdate
	}

	s := &TaskStatus{
		Name:          stack[0],
		ActivityStack: stack,
		Idle:          cur.idle,
		ProgressRx:    prog.rx,
		ProgressTx:    prog.tx,
		LogEntries:    prog.logEntries,
		MaxLogLevel:   maxLevel,
		LastUpdate:    lastUpdate,
	}

	return s
}

// Finish a sub-activity.
// Corresponds to a preceding call to t.Enter()
func (t *Task) Finish() {
	t.rwl.Lock()
	defer t.rwl.Unlock()
	top := t.activities.Front()
	if top.Next() == nil {
		return // cannot remove root activity
	}
	t.activities.Remove(top)
	t.activitiesLastUpdate = time.Now()

	// prometheus
	front := t.activities.Front()
	if front != nil && front == t.activities.Back() {
		idleAct := front.Value.(*taskActivity)
		if !idleAct.idle {
			panic("inconsistent implementation")
		}
		progress := idleAct.progress.Read()
		non_idle_time := t.activitiesLastUpdate.Sub(progress.creation) // use same time
		prom.taskLastActiveDuration.WithLabelValues(
			t.parent.JobName(),
			t.parent.JobType().String(),
			t.name).Set(non_idle_time.Seconds())
	}

}

// Returns a logger derived from the logger passed to the constructor function.
// The logger's task field contains the current activity stack joined by '.'.
func (t *Task) Log() *logger.Logger {
	t.rwl.RLock()
	defer t.rwl.RUnlock()
	// FIXME should influence TaskStatus's LastUpdate field
	return t.cur().logger
}

// implement logger.Outlet interface
func (t *Task) WriteEntry(entry logger.Entry) error {
	t.rwl.RLock()
	defer t.rwl.RUnlock()
	t.cur().progress.UpdateLogEntry(entry)

	prom.taskLogEntries.WithLabelValues(
		t.parent.JobName(),
		t.parent.JobType().String(),
		t.name,
		entry.Level.String()).
		Inc()

	return nil
}

type IOProgressUpdater struct {
	r io.Reader
	p *taskProgress
}

func (u *IOProgressUpdater) Read(p []byte) (n int, err error) {
	n, err = u.r.Read(p)
	u.p.UpdateIO(int64(n), 0)
	return

}
