package replication

import (
	"context"
	"fmt"
	"github.com/zrepl/zrepl/logger"
	"io"
	"net"
	"sort"
	"time"
)

type ReplicationEndpoint interface {
	// Does not include placeholder filesystems
	ListFilesystems(ctx context.Context) ([]*Filesystem, error)
	ListFilesystemVersions(ctx context.Context, fs string) ([]*FilesystemVersion, error) // fix depS
	Send(ctx context.Context, r *SendReq) (*SendRes, io.ReadCloser, error)
	Receive(ctx context.Context, r *ReceiveReq, sendStream io.ReadCloser) error
}

type FilteredError struct{ fs string }

func NewFilteredError(fs string) FilteredError {
	return FilteredError{fs}
}

func (f FilteredError) Error() string { return "endpoint does not allow access to filesystem " + f.fs }

type ReplicationMode int

const (
	ReplicationModePull ReplicationMode = iota
	ReplicationModePush
)

type EndpointPair struct {
	a, b ReplicationEndpoint
	m    ReplicationMode
}

func NewEndpointPairPull(sender, receiver ReplicationEndpoint) EndpointPair {
	return EndpointPair{sender, receiver, ReplicationModePull}
}

func NewEndpointPairPush(sender, receiver ReplicationEndpoint) EndpointPair {
	return EndpointPair{receiver, sender, ReplicationModePush}
}

func (p EndpointPair) Sender() ReplicationEndpoint {
	switch p.m {
	case ReplicationModePull:
		return p.a
	case ReplicationModePush:
		return p.b
	}
	panic("should not be reached")
	return nil
}

func (p EndpointPair) Receiver() ReplicationEndpoint {
	switch p.m {
	case ReplicationModePull:
		return p.b
	case ReplicationModePush:
		return p.a
	}
	panic("should not be reached")
	return nil
}

func (p EndpointPair) Mode() ReplicationMode {
	return p.m
}

type contextKey int

const (
	contextKeyLog contextKey = iota
)

//type Logger interface {
//	Infof(fmt string, args ...interface{})
//	Errorf(fmt string, args ...interface{})
//}

//var _ Logger = nullLogger{}

//type nullLogger struct{}
//
//func (nullLogger) Infof(fmt string, args ...interface{}) {}
//func (nullLogger) Errorf(fmt string, args ...interface{}) {}

type Logger = logger.Logger

func ContextWithLogger(ctx context.Context, l Logger) context.Context {
	return context.WithValue(ctx, contextKeyLog, l)
}

func getLogger(ctx context.Context) Logger {
	l, ok := ctx.Value(contextKeyLog).(Logger)
	if !ok {
		l = logger.NewNullLogger()
	}
	return l
}

type replicationStep struct {
	from, to *FilesystemVersion
	fswork   *replicateFSWork
}

func (s *replicationStep) String() string {
	if s.from == nil { // FIXME: ZFS semantics are that to is nil on non-incremental send
		return fmt.Sprintf("%s%s (full)", s.fswork.fs.Path, s.to.RelName())
	} else {
		return fmt.Sprintf("%s(%s => %s)", s.fswork.fs.Path, s.from.RelName(), s.to.RelName())
	}
}

func newReplicationStep(from, to *FilesystemVersion) *replicationStep {
	return &replicationStep{from: from, to: to}
}

type replicateFSWork struct {
	fs          *Filesystem
	steps       []*replicationStep
	currentStep int
	errorCount  int
}

func newReplicateFSWork(fs *Filesystem) *replicateFSWork {
	if fs == nil {
		panic("implementation error")
	}
	return &replicateFSWork{
		fs:    fs,
		steps: make([]*replicationStep, 0),
	}
}

func newReplicateFSWorkWithConflict(fs *Filesystem, conflict error) *replicateFSWork {
	// FIXME ignore conflict for now, but will be useful later when we make the replicationPlan exportable
	return &replicateFSWork{
		fs:    fs,
		steps: make([]*replicationStep, 0),
	}
}

func (r *replicateFSWork) AddStep(step *replicationStep) {
	if step == nil {
		panic("implementation error")
	}
	if step.fswork != nil {
		panic("implementation error")
	}
	step.fswork = r
	r.steps = append(r.steps, step)
}

func (w *replicateFSWork) CurrentStepDate() time.Time {
	if len(w.steps) == 0 {
		return time.Time{}
	}
	toTime, err := w.steps[w.currentStep].to.CreationAsTime()
	if err != nil {
		panic(err) // implementation inconsistent: should not admit invalid FilesystemVersion objects
	}
	return toTime
}

func (w *replicateFSWork) CurrentStep() *replicationStep {
	if w.currentStep >= len(w.steps) {
		return nil
	}
	return w.steps[w.currentStep]
}

func (w *replicateFSWork) CompleteStep() {
	w.currentStep++
}

type replicationPlan struct {
	fsws []*replicateFSWork
}

func newReplicationPlan() *replicationPlan {
	return &replicationPlan{
		fsws: make([]*replicateFSWork, 0),
	}
}

func (p *replicationPlan) addWork(work *replicateFSWork) {
	p.fsws = append(p.fsws, work)
}

func (p *replicationPlan) executeOldestFirst(ctx context.Context, doStep func(fs *Filesystem, from, to *FilesystemVersion) tryRes) {
	log := getLogger(ctx)

	for {
		select {
		case <-ctx.Done():
			log.WithError(ctx.Err()).Info("aborting replication due to context error")
			return
		default:
		}

		// FIXME poor man's nested priority queue
		pending := make([]*replicateFSWork, 0, len(p.fsws))
		for _, fsw := range p.fsws {
			if fsw.CurrentStep() != nil {
				pending = append(pending, fsw)
			}
		}
		sort.Slice(pending, func(i, j int) bool {
			if pending[i].errorCount == pending[j].errorCount {
				return pending[i].CurrentStepDate().Before(pending[j].CurrentStepDate())
			}
			return pending[i].errorCount < pending[j].errorCount
		})
		// pending is now sorted ascending by errorCount,CurrentStep().Creation

		if len(pending) == 0 {
			log.Info("replication complete")
			return
		}

		fsw := pending[0]
		step := fsw.CurrentStep()
		if step == nil {
			panic("implementation error")
		}

		log.WithField("step", step).Info("begin replication step")
		res := doStep(step.fswork.fs, step.from, step.to)

		if res.done {
			log.Info("replication step successful")
			fsw.errorCount = 0
			fsw.CompleteStep()
		} else {
			log.Error("replication step failed, queuing for retry result")
			fsw.errorCount++
		}

	}

}

func resolveConflict(conflict error) (path []*FilesystemVersion, msg string) {
	if noCommonAncestor, ok := conflict.(*ConflictNoCommonAncestor); ok {
		if len(noCommonAncestor.SortedReceiverVersions) == 0 {
			// FIXME hard-coded replication policy: most recent
			// snapshot as source
			var mostRecentSnap *FilesystemVersion
			for n := len(noCommonAncestor.SortedSenderVersions) - 1; n >= 0; n-- {
				if noCommonAncestor.SortedSenderVersions[n].Type == FilesystemVersion_Snapshot {
					mostRecentSnap = noCommonAncestor.SortedSenderVersions[n]
					break
				}
			}
			if mostRecentSnap == nil {
				return nil, "no snapshots available on sender side"
			}
			return []*FilesystemVersion{mostRecentSnap}, fmt.Sprintf("start replication at most recent snapshot %s", mostRecentSnap.RelName())
		}
	}
	return nil, "no automated way to handle conflict type"
}

// Replicate replicates filesystems from ep.Sender() to ep.Receiver().
//
// All filesystems presented by the sending side are replicated,
// unless the receiver rejects a Receive request with a *FilteredError.
//
// If an error occurs when replicating a filesystem, that error is logged to the logger in ctx.
// Replicate continues with the replication of the remaining file systems.
// Depending on the type of error, failed replications are retried in an unspecified order (currently FIFO).
func Replicate(ctx context.Context, ep EndpointPair) {

	log := getLogger(ctx)

	retryPlanTicker := time.NewTicker(15 * time.Second) // FIXME make configurable
	defer retryPlanTicker.Stop()

	var (
		plan *replicationPlan
		res  tryRes
	)
	for {
		log.Info("build replication plan")
		plan, res = tryBuildReplicationPlan(ctx, ep)
		if plan != nil {
			break
		}
		log.WithField("result", res).Error("building replication plan failed, wait for retry timer result")
		select {
		case <-ctx.Done():
			log.WithError(ctx.Err()).Info("aborting replication because context is done")
			return
		case <-retryPlanTicker.C:
			// TODO also accept an external channel that allows us to tick
		}
	}
	retryPlanTicker.Stop()

	mainlog := log
	plan.executeOldestFirst(ctx, func(fs *Filesystem, from, to *FilesystemVersion) tryRes {

		log := mainlog.WithField("filesystem", fs.Path)

		// FIXME refresh fs resume token
		fs.ResumeToken = ""

		var sr *SendReq
		if fs.ResumeToken != "" {
			sr = &SendReq{
				Filesystem:  fs.Path,
				ResumeToken: fs.ResumeToken,
			}
		} else if from == nil {
			sr = &SendReq{
				Filesystem: fs.Path,
				From:       to.RelName(), // FIXME fix protocol to use To, like zfs does internally
			}
		} else {
			sr = &SendReq{
				Filesystem: fs.Path,
				From:       from.RelName(),
				To:         to.RelName(),
			}
		}

		log.WithField("request", sr).Debug("initiate send request")
		sres, sstream, err := ep.Sender().Send(ctx, sr)
		if err != nil {
			log.WithError(err).Error("send request failed")
			return tryResFromEndpointError(err)
		}
		if sstream == nil {
			log.Error("send request did not return a stream, broken endpoint implementation")
			return tryRes{unfixable: true}
		}

		rr := &ReceiveReq{
			Filesystem:       fs.Path,
			ClearResumeToken: !sres.UsedResumeToken,
		}
		log.WithField("request", rr).Debug("initiate receive request")
		err = ep.Receiver().Receive(ctx, rr, sstream)
		if err != nil {
			log.WithError(err).Error("receive request failed (might also be error on sender)")
			sstream.Close()
			// This failure could be due to
			// 	- an unexpected exit of ZFS on the sending side
			//  - an unexpected exit of ZFS on the receiving side
			//  - a connectivity issue
			return tryResFromEndpointError(err)
		}
		log.Info("receive finished")
		return tryRes{done: true}

	})

}

type tryRes struct {
	done      bool
	retry     bool
	unfixable bool
}

func tryResFromEndpointError(err error) tryRes {
	if _, ok := err.(net.Error); ok {
		return tryRes{retry: true}
	}
	return tryRes{unfixable: true}
}

func tryBuildReplicationPlan(ctx context.Context, ep EndpointPair) (*replicationPlan, tryRes) {

	log := getLogger(ctx)

	early := func(err error) (*replicationPlan, tryRes) {
		return nil, tryResFromEndpointError(err)
	}

	sfss, err := ep.Sender().ListFilesystems(ctx)
	if err != nil {
		log.WithError(err).Error("error listing sender filesystems")
		return early(err)
	}

	rfss, err := ep.Receiver().ListFilesystems(ctx)
	if err != nil {
		log.WithError(err).Error("error listing receiver filesystems")
		return early(err)
	}

	plan := newReplicationPlan()
	mainlog := log
	for _, fs := range sfss {

		log := mainlog.WithField("filesystem", fs.Path)

		log.Info("assessing filesystem")

		sfsvs, err := ep.Sender().ListFilesystemVersions(ctx, fs.Path)
		if err != nil {
			log.WithError(err).Error("cannot get remote filesystem versions")
			return early(err)
		}

		if len(sfsvs) <= 1 {
			log.Error("sender does not have any versions")
			return nil, tryRes{unfixable: true}
		}

		receiverFSExists := false
		for _, rfs := range rfss {
			if rfs.Path == fs.Path {
				receiverFSExists = true
			}
		}

		var rfsvs []*FilesystemVersion
		if receiverFSExists {
			rfsvs, err = ep.Receiver().ListFilesystemVersions(ctx, fs.Path)
			if err != nil {
				if _, ok := err.(FilteredError); ok {
					log.Info("receiver ignores filesystem")
					continue
				}
				log.WithError(err).Error("receiver error")
				return early(err)
			}
		} else {
			rfsvs = []*FilesystemVersion{}
		}

		path, conflict := IncrementalPath(rfsvs, sfsvs)
		if conflict != nil {
			var msg string
			path, msg = resolveConflict(conflict) // no shadowing allowed!
			if path != nil {
				log.WithField("conflict", conflict).Info("conflict")
				log.WithField("resolution", msg).Info("automatically resolved")
			} else {
				log.WithField("conflict", conflict).Error("conflict")
				log.WithField("problem", msg).Error("cannot resolve conflict")
			}
		}
		if path == nil {
			plan.addWork(newReplicateFSWorkWithConflict(fs, conflict))
			continue
		}

		w := newReplicateFSWork(fs)
		if len(path) == 1 {
			step := newReplicationStep(nil, path[0])
			w.AddStep(step)
		} else {
			for i := 0; i < len(path)-1; i++ {
				step := newReplicationStep(path[i], path[i+1])
				w.AddStep(step)
			}
		}
		plan.addWork(w)

	}

	return plan, tryRes{done: true}
}
