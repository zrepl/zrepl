package logic

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/zrepl/zrepl/replication/driver"
	. "github.com/zrepl/zrepl/replication/logic/diff"
	"github.com/zrepl/zrepl/replication/logic/pdu"
	"github.com/zrepl/zrepl/replication/report"
	"github.com/zrepl/zrepl/util/bytecounter"
	"github.com/zrepl/zrepl/util/chainlock"
	"github.com/zrepl/zrepl/util/envconst"
	"github.com/zrepl/zrepl/util/semaphore"
	"github.com/zrepl/zrepl/zfs"
)

// Endpoint represents one side of the replication.
//
// An endpoint is either in Sender or Receiver mode, represented by the correspondingly
// named interfaces defined in this package.
type Endpoint interface {
	// Does not include placeholder filesystems
	ListFilesystems(ctx context.Context, req *pdu.ListFilesystemReq) (*pdu.ListFilesystemRes, error)
	ListFilesystemVersions(ctx context.Context, req *pdu.ListFilesystemVersionsReq) (*pdu.ListFilesystemVersionsRes, error)
	DestroySnapshots(ctx context.Context, req *pdu.DestroySnapshotsReq) (*pdu.DestroySnapshotsRes, error)
	WaitForConnectivity(ctx context.Context) error
}

type Sender interface {
	Endpoint
	// If a non-nil io.ReadCloser is returned, it is guaranteed to be closed before
	// any next call to the parent github.com/zrepl/zrepl/replication.Endpoint.
	// If the send request is for dry run the io.ReadCloser will be nil
	Send(ctx context.Context, r *pdu.SendReq) (*pdu.SendRes, zfs.StreamCopier, error)
	ReplicationCursor(ctx context.Context, req *pdu.ReplicationCursorReq) (*pdu.ReplicationCursorRes, error)
}

type Receiver interface {
	Endpoint
	// Receive sends r and sendStream (the latter containing a ZFS send stream)
	// to the parent github.com/zrepl/zrepl/replication.Endpoint.
	Receive(ctx context.Context, req *pdu.ReceiveReq, receive zfs.StreamCopier) (*pdu.ReceiveRes, error)
}

type Planner struct {
	sender   Sender
	receiver Receiver

	promSecsPerState    *prometheus.HistogramVec // labels: state
	promBytesReplicated *prometheus.CounterVec   // labels: filesystem
}

func (p *Planner) Plan(ctx context.Context) ([]driver.FS, error) {
	fss, err := p.doPlanning(ctx)
	if err != nil {
		return nil, err
	}
	dfss := make([]driver.FS, len(fss))
	for i := range dfss {
		dfss[i] = fss[i]
	}
	return dfss, nil
}

func (p *Planner) WaitForConnectivity(ctx context.Context) error {
	var wg sync.WaitGroup
	doPing := func(endpoint Endpoint, errOut *error) {
		defer wg.Done()
		err := endpoint.WaitForConnectivity(ctx)
		if err != nil {
			*errOut = err
		} else {
			*errOut = nil
		}
	}
	wg.Add(2)
	var senderErr, receiverErr error
	go doPing(p.sender, &senderErr)
	go doPing(p.receiver, &receiverErr)
	wg.Wait()
	if senderErr == nil && receiverErr == nil {
		return nil
	} else if senderErr != nil && receiverErr != nil {
		if senderErr.Error() == receiverErr.Error() {
			return fmt.Errorf("sender and receiver are not reachable: %s", senderErr.Error())
		} else {
			return fmt.Errorf("sender and receiver are not reachable:\n  sender: %s\n  receiver: %s", senderErr, receiverErr)
		}
	} else {
		var side string
		var err *error
		if senderErr != nil {
			side = "sender"
			err = &senderErr
		} else {
			side = "receiver"
			err = &receiverErr
		}
		return fmt.Errorf("%s is not reachable: %s", side, *err)
	}
}

type Filesystem struct {
	sender   Sender
	receiver Receiver

	Path                string // compat
	receiverFS          *pdu.Filesystem
	promBytesReplicated prometheus.Counter // compat

	sizeEstimateRequestSem *semaphore.S
}

func (f *Filesystem) EqualToPreviousAttempt(other driver.FS) bool {
	g, ok := other.(*Filesystem)
	if !ok {
		return false
	}
	// TODO: use GUIDs (issued by zrepl, not those from ZFS)
	return f.Path == g.Path
}

func (f *Filesystem) PlanFS(ctx context.Context) ([]driver.Step, error) {
	steps, err := f.doPlanning(ctx)
	if err != nil {
		return nil, err
	}
	dsteps := make([]driver.Step, len(steps))
	for i := range dsteps {
		dsteps[i] = steps[i]
	}
	return dsteps, nil
}
func (f *Filesystem) ReportInfo() *report.FilesystemInfo {
	return &report.FilesystemInfo{Name: f.Path} // FIXME compat name
}

type Step struct {
	sender   Sender
	receiver Receiver

	parent   *Filesystem
	from, to *pdu.FilesystemVersion // compat

	expectedSize int64 // 0 means no size estimate present / possible

	// byteCounter is nil initially, and set later in Step.doReplication
	// => concurrent read of that pointer from Step.ReportInfo must be protected
	byteCounter    bytecounter.StreamCopier
	byteCounterMtx chainlock.L
}

func (s *Step) TargetEquals(other driver.Step) bool {
	t, ok := other.(*Step)
	if !ok {
		return false
	}
	if !s.parent.EqualToPreviousAttempt(t.parent) {
		panic("Step interface promise broken: parent filesystems must be same")
	}
	return s.from.GetGuid() == t.from.GetGuid() &&
		s.to.GetGuid() == t.to.GetGuid()
}

func (s *Step) TargetDate() time.Time {
	return s.to.SnapshotTime() // FIXME compat name
}

func (s *Step) Step(ctx context.Context) error {
	return s.doReplication(ctx)
}

func (s *Step) ReportInfo() *report.StepInfo {

	// get current byteCounter value
	var byteCounter int64
	s.byteCounterMtx.Lock()
	if s.byteCounter != nil {
		byteCounter = s.byteCounter.Count()
	}
	s.byteCounterMtx.Unlock()

	// FIXME stick to zfs convention of from and to
	from := ""
	if s.from != nil {
		from = s.from.RelName()
	}
	return &report.StepInfo{
		From:            from,
		To:              s.to.RelName(),
		BytesExpected:   s.expectedSize,
		BytesReplicated: byteCounter,
	}
}

func NewPlanner(secsPerState *prometheus.HistogramVec, bytesReplicated *prometheus.CounterVec, sender Sender, receiver Receiver) *Planner {
	return &Planner{
		sender:              sender,
		receiver:            receiver,
		promSecsPerState:    secsPerState,
		promBytesReplicated: bytesReplicated,
	}
}
func resolveConflict(conflict error) (path []*pdu.FilesystemVersion, msg string) {
	if noCommonAncestor, ok := conflict.(*ConflictNoCommonAncestor); ok {
		if len(noCommonAncestor.SortedReceiverVersions) == 0 {
			// TODO this is hard-coded replication policy: most recent snapshot as source
			var mostRecentSnap *pdu.FilesystemVersion
			for n := len(noCommonAncestor.SortedSenderVersions) - 1; n >= 0; n-- {
				if noCommonAncestor.SortedSenderVersions[n].Type == pdu.FilesystemVersion_Snapshot {
					mostRecentSnap = noCommonAncestor.SortedSenderVersions[n]
					break
				}
			}
			if mostRecentSnap == nil {
				return nil, "no snapshots available on sender side"
			}
			return []*pdu.FilesystemVersion{mostRecentSnap}, fmt.Sprintf("start replication at most recent snapshot %s", mostRecentSnap.RelName())
		}
	}
	return nil, "no automated way to handle conflict type"
}

func (p *Planner) doPlanning(ctx context.Context) ([]*Filesystem, error) {

	log := getLogger(ctx)

	log.Info("start planning")

	slfssres, err := p.sender.ListFilesystems(ctx, &pdu.ListFilesystemReq{})
	if err != nil {
		log.WithError(err).WithField("errType", fmt.Sprintf("%T", err)).Error("error listing sender filesystems")
		return nil, err
	}
	sfss := slfssres.GetFilesystems()
	// no progress here since we could run in a live-lock on connectivity issues

	rlfssres, err := p.receiver.ListFilesystems(ctx, &pdu.ListFilesystemReq{})
	if err != nil {
		log.WithError(err).WithField("errType", fmt.Sprintf("%T", err)).Error("error listing receiver filesystems")
		return nil, err
	}
	rfss := rlfssres.GetFilesystems()

	sizeEstimateRequestSem := semaphore.New(envconst.Int64("ZREPL_REPLICATION_MAX_CONCURRENT_SIZE_ESTIMATE", 4))

	q := make([]*Filesystem, 0, len(sfss))
	for _, fs := range sfss {

		var receiverFS *pdu.Filesystem
		for _, rfs := range rfss {
			if rfs.Path == fs.Path {
				receiverFS = rfs
			}
		}

		ctr := p.promBytesReplicated.WithLabelValues(fs.Path)

		q = append(q, &Filesystem{
			sender:                 p.sender,
			receiver:               p.receiver,
			Path:                   fs.Path,
			receiverFS:             receiverFS,
			promBytesReplicated:    ctr,
			sizeEstimateRequestSem: sizeEstimateRequestSem,
		})
	}

	return q, nil
}

func (fs *Filesystem) doPlanning(ctx context.Context) ([]*Step, error) {

	log := getLogger(ctx).WithField("filesystem", fs.Path)

	log.Debug("assessing filesystem")

	sfsvsres, err := fs.sender.ListFilesystemVersions(ctx, &pdu.ListFilesystemVersionsReq{Filesystem: fs.Path})
	if err != nil {
		log.WithError(err).Error("cannot get remote filesystem versions")
		return nil, err
	}
	sfsvs := sfsvsres.GetVersions()

	if len(sfsvs) < 1 {
		err := errors.New("sender does not have any versions")
		log.Error(err.Error())
		return nil, err
	}

	var rfsvs []*pdu.FilesystemVersion
	if fs.receiverFS != nil && !fs.receiverFS.GetIsPlaceholder() {
		rfsvsres, err := fs.receiver.ListFilesystemVersions(ctx, &pdu.ListFilesystemVersionsReq{Filesystem: fs.Path})
		if err != nil {
			log.WithError(err).Error("receiver error")
			return nil, err
		}
		rfsvs = rfsvsres.GetVersions()
	} else {
		rfsvs = []*pdu.FilesystemVersion{}
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
	if len(path) == 0 {
		return nil, conflict
	}

	steps := make([]*Step, 0, len(path))
	// FIXME unify struct declarations => initializer?
	if len(path) == 1 {
		steps = append(steps, &Step{
			parent:   fs,
			sender:   fs.sender,
			receiver: fs.receiver,
			from:     nil,
			to:       path[0],
		})
	} else {
		for i := 0; i < len(path)-1; i++ {
			steps = append(steps, &Step{
				parent:   fs,
				sender:   fs.sender,
				receiver: fs.receiver,
				from:     path[i],
				to:       path[i+1],
			})
		}
	}

	log.Debug("compute send size estimate")
	errs := make(chan error, len(steps))
	var wg sync.WaitGroup
	fanOutCtx, fanOutCancel := context.WithCancel(ctx)
	defer fanOutCancel()
	for _, step := range steps {
		wg.Add(1)
		go func(step *Step) {
			defer wg.Done()

			// TODO instead of the semaphore, rely on resource-exhaustion signaled by the remote endpoint to limit size-estimate requests
			// Send is handled over rpc/dataconn ATM, which doesn't support the resource exhaustion status codes that gRPC defines
			guard, err := fs.sizeEstimateRequestSem.Acquire(fanOutCtx)
			if err != nil {
				fanOutCancel()
				return
			}
			defer guard.Release()

			err = step.updateSizeEstimate(fanOutCtx)
			if err != nil {
				log.WithError(err).WithField("step", step).Error("error computing size estimate")
				fanOutCancel()
			}
			errs <- err
		}(step)
	}
	wg.Wait()
	close(errs)
	var significantErr error = nil
	for err := range errs {
		if err != nil {
			if significantErr == nil || significantErr == context.Canceled {
				significantErr = err
			}
		}
	}
	if significantErr != nil {
		return nil, significantErr
	}

	log.Debug("filesystem planning finished")
	return steps, nil
}

// type FilesystemsReplicationFailedError struct {
// 	FilesystemsWithError []*fsrep.Replication
// }

// func (e FilesystemsReplicationFailedError) Error() string {
// 	allSame := true
// 	lastErr := e.FilesystemsWithError[0].Err().Error()
// 	for _, fs := range e.FilesystemsWithError {
// 		fsErr := fs.Err().Error()
// 		allSame = allSame && lastErr == fsErr
// 	}

// 	fsstr := "multiple filesystems"
// 	if len(e.FilesystemsWithError) == 1 {
// 		fsstr = fmt.Sprintf("filesystem %s", e.FilesystemsWithError[0].FS())
// 	}
// 	errorStr := lastErr
// 	if !allSame {
// 		errorStr = "multiple different errors"
// 	}
// 	return fmt.Sprintf("%s could not be replicated: %s", fsstr, errorStr)
// }

func (s *Step) updateSizeEstimate(ctx context.Context) error {

	log := getLogger(ctx)

	sr := s.buildSendRequest(true)

	log.Debug("initiate dry run send request")
	sres, _, err := s.sender.Send(ctx, sr)
	if err != nil {
		log.WithError(err).Error("dry run send request failed")
		return err
	}
	s.expectedSize = sres.ExpectedSize
	return nil
}

func (s *Step) buildSendRequest(dryRun bool) (sr *pdu.SendReq) {
	fs := s.parent.Path
	if s.from == nil {
		sr = &pdu.SendReq{
			Filesystem: fs,
			To:         s.to.RelName(),
			DryRun:     dryRun,
		}
	} else {
		sr = &pdu.SendReq{
			Filesystem: fs,
			From:       s.from.RelName(),
			To:         s.to.RelName(),
			DryRun:     dryRun,
		}
	}
	return sr
}

func (s *Step) doReplication(ctx context.Context) error {

	fs := s.parent.Path

	log := getLogger(ctx)
	sr := s.buildSendRequest(false)

	log.Debug("initiate send request")
	sres, sstreamCopier, err := s.sender.Send(ctx, sr)
	if err != nil {
		log.WithError(err).Error("send request failed")
		return err
	}
	if sstreamCopier == nil {
		err := errors.New("send request did not return a stream, broken endpoint implementation")
		return err
	}
	defer sstreamCopier.Close()

	// Install a byte counter to track progress + for status report
	byteCountingStream := bytecounter.NewStreamCopier(sstreamCopier)
	s.byteCounterMtx.Lock()
	s.byteCounter = byteCountingStream
	s.byteCounterMtx.Unlock()
	defer func() {
		defer s.byteCounterMtx.Lock().Unlock()
		s.parent.promBytesReplicated.Add(float64(s.byteCounter.Count()))
	}()

	rr := &pdu.ReceiveReq{
		Filesystem:       fs,
		ClearResumeToken: !sres.UsedResumeToken,
	}
	log.Debug("initiate receive request")
	_, err = s.receiver.Receive(ctx, rr, byteCountingStream)
	if err != nil {
		log.
			WithError(err).
			WithField("errType", fmt.Sprintf("%T", err)).
			Error("receive request failed (might also be error on sender)")
		// This failure could be due to
		// 	- an unexpected exit of ZFS on the sending side
		//  - an unexpected exit of ZFS on the receiving side
		//  - a connectivity issue
		return err
	}
	log.Debug("receive finished")

	log.Debug("advance replication cursor")
	req := &pdu.ReplicationCursorReq{
		Filesystem: fs,
		Op: &pdu.ReplicationCursorReq_Set{
			Set: &pdu.ReplicationCursorReq_SetOp{
				Snapshot: s.to.GetName(),
			},
		},
	}
	_, err = s.sender.ReplicationCursor(ctx, req)
	if err != nil {
		log.WithError(err).Error("error advancing replication cursor")
		// If this fails and replication planning restarts, the diff algorithm will find
		// that cursor out of place. This is not a problem because then, it would just use another FS
		// However, we FIXME have no means to just update the cursor in a
		// second replication attempt right after this one where we don't have new snaps yet
		return err
	}

	return err
}

func (s *Step) String() string {
	if s.from == nil { // FIXME: ZFS semantics are that to is nil on non-incremental send
		return fmt.Sprintf("%s%s (full)", s.parent.Path, s.to.RelName())
	} else {
		return fmt.Sprintf("%s(%s => %s)", s.parent.Path, s.from.RelName(), s.to.RelName())
	}
}
