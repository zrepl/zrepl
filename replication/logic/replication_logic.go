package logic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/zrepl/zrepl/daemon/logging/trace"

	"github.com/zrepl/zrepl/logger"
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
	Send(ctx context.Context, r *pdu.SendReq) (*pdu.SendRes, io.ReadCloser, error)
	SendCompleted(ctx context.Context, r *pdu.SendCompletedReq) (*pdu.SendCompletedRes, error)
	ReplicationCursor(ctx context.Context, req *pdu.ReplicationCursorReq) (*pdu.ReplicationCursorRes, error)
}

type Receiver interface {
	Endpoint
	// Receive sends r and sendStream (the latter containing a ZFS send stream)
	// to the parent github.com/zrepl/zrepl/replication.Endpoint.
	Receive(ctx context.Context, req *pdu.ReceiveReq, receive io.ReadCloser) (*pdu.ReceiveRes, error)
}

type Planner struct {
	sender   Sender
	receiver Receiver
	policy   PlannerPolicy

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
		ctx, endTask := trace.WithTaskFromStack(ctx)
		defer endTask()
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
	policy   PlannerPolicy // immutable, it's .ReplicationConfig member is a pointer and copied into messages

	Path                 string             // compat
	receiverFS, senderFS *pdu.Filesystem    // receiverFS may be nil, senderFS never nil
	promBytesReplicated  prometheus.Counter // compat

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

	parent      *Filesystem
	from, to    *pdu.FilesystemVersion // from may be nil, indicating full send
	encrypt     tri
	resumeToken string // empty means no resume token shall be used

	expectedSize int64 // 0 means no size estimate present / possible

	// byteCounter is nil initially, and set later in Step.doReplication
	// => concurrent read of that pointer from Step.ReportInfo must be protected
	byteCounter    bytecounter.ReadCloser
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

	from := ""
	if s.from != nil {
		from = s.from.RelName()
	}
	var encrypted report.EncryptedEnum
	switch s.encrypt {
	case DontCare:
		encrypted = report.EncryptedSenderDependent
	case True:
		encrypted = report.EncryptedTrue
	case False:
		encrypted = report.EncryptedFalse
	default:
		panic(fmt.Sprintf("unknown variant %s", s.encrypt))
	}
	return &report.StepInfo{
		From:            from,
		To:              s.to.RelName(),
		Resumed:         s.resumeToken != "",
		Encrypted:       encrypted,
		BytesExpected:   s.expectedSize,
		BytesReplicated: byteCounter,
	}
}

func NewPlanner(secsPerState *prometheus.HistogramVec, bytesReplicated *prometheus.CounterVec, sender Sender, receiver Receiver, policy PlannerPolicy) *Planner {
	return &Planner{
		sender:              sender,
		receiver:            receiver,
		policy:              policy,
		promSecsPerState:    secsPerState,
		promBytesReplicated: bytesReplicated,
	}
}
func resolveConflict(conflict error) (path []*pdu.FilesystemVersion, msg string) {
	if noCommonAncestor, ok := conflict.(*ConflictNoCommonAncestor); ok {
		if len(noCommonAncestor.SortedReceiverVersions) == 0 {
			// TODO this is hard-coded replication policy: most recent snapshot as source
			// NOTE: Keep in sync with listStaleFiltering, it depends on this hard-coded assumption
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

		var ctr prometheus.Counter
		if p.promBytesReplicated != nil {
			ctr = p.promBytesReplicated.WithLabelValues(fs.Path)
		}

		q = append(q, &Filesystem{
			sender:                 p.sender,
			receiver:               p.receiver,
			policy:                 p.policy,
			Path:                   fs.Path,
			senderFS:               fs,
			receiverFS:             receiverFS,
			promBytesReplicated:    ctr,
			sizeEstimateRequestSem: sizeEstimateRequestSem,
		})
	}

	return q, nil
}

func (fs *Filesystem) doPlanning(ctx context.Context) ([]*Step, error) {

	log := func(ctx context.Context) logger.Logger {
		return getLogger(ctx).WithField("filesystem", fs.Path)
	}

	log(ctx).Debug("assessing filesystem")

	if fs.policy.EncryptedSend == True && !fs.senderFS.GetIsEncrypted() {
		return nil, fmt.Errorf("sender filesystem is not encrypted but policy mandates encrypted send")
	}

	sfsvsres, err := fs.sender.ListFilesystemVersions(ctx, &pdu.ListFilesystemVersionsReq{Filesystem: fs.Path})
	if err != nil {
		log(ctx).WithError(err).Error("cannot get remote filesystem versions")
		return nil, err
	}
	sfsvs := sfsvsres.GetVersions()

	if len(sfsvs) < 1 {
		err := errors.New("sender does not have any versions")
		log(ctx).Error(err.Error())
		return nil, err
	}

	var rfsvs []*pdu.FilesystemVersion
	if fs.receiverFS != nil && !fs.receiverFS.GetIsPlaceholder() {
		rfsvsres, err := fs.receiver.ListFilesystemVersions(ctx, &pdu.ListFilesystemVersionsReq{Filesystem: fs.Path})
		if err != nil {
			log(ctx).WithError(err).Error("receiver error")
			return nil, err
		}
		rfsvs = rfsvsres.GetVersions()
	} else {
		rfsvs = []*pdu.FilesystemVersion{}
	}

	var resumeToken *zfs.ResumeToken
	var resumeTokenRaw string
	if fs.receiverFS != nil && fs.receiverFS.ResumeToken != "" {
		resumeTokenRaw = fs.receiverFS.ResumeToken // shadow
		log(ctx).WithField("receiverFS.ResumeToken", resumeTokenRaw).Debug("decode receiver fs resume token")
		resumeToken, err = zfs.ParseResumeToken(ctx, resumeTokenRaw) // shadow
		if err != nil {
			// TODO in theory, we could do replication without resume token, but that would mean that
			// we need to discard the resumable state on the receiver's side.
			// Would be easy by setting UsedResumeToken=false in the RecvReq ...
			// FIXME / CHECK semantics UsedResumeToken if SendReq.ResumeToken == ""
			log(ctx).WithError(err).Error("cannot decode resume token, aborting")
			return nil, err
		}
		log(ctx).WithField("token", resumeToken).Debug("decode resume token")
	}

	var steps []*Step
	// build the list of replication steps
	//
	// prefer to resume any started replication instead of starting over with a normal IncrementalPath
	//
	// look for the step encoded in the resume token in the sender's version
	// if we find that step:
	//   1. use it as first step (including resume token)
	//   2. compute subsequent steps by computing incremental path from the token.To version on
	//      ...
	//      that's actually equivalent to simply cutting off earlier versions from rfsvs and sfsvs
	if resumeToken != nil {

		sfsvs := SortVersionListByCreateTXGThenBookmarkLTSnapshot(sfsvs)

		var fromVersion, toVersion *pdu.FilesystemVersion
		var toVersionIdx int
		for idx, sfsv := range sfsvs {
			if resumeToken.HasFromGUID && sfsv.Guid == resumeToken.FromGUID {
				if fromVersion != nil && fromVersion.Type == pdu.FilesystemVersion_Snapshot {
					// prefer snapshots over bookmarks for size estimation
				} else {
					fromVersion = sfsv
				}
			}
			if resumeToken.HasToGUID && sfsv.Guid == resumeToken.ToGUID && sfsv.Type == pdu.FilesystemVersion_Snapshot {
				// `toversion` must always be a snapshot
				toVersion, toVersionIdx = sfsv, idx
			}
		}

		encryptionMatches := false
		switch fs.policy.EncryptedSend {
		case True:
			encryptionMatches = resumeToken.RawOK && resumeToken.CompressOK
		case False:
			encryptionMatches = !resumeToken.RawOK && !resumeToken.CompressOK
		case DontCare:
			encryptionMatches = true
		}

		log(ctx).WithField("fromVersion", fromVersion).
			WithField("toVersion", toVersion).
			WithField("encryptionMatches", encryptionMatches).
			Debug("result of resume-token-matching to sender's versions")

		if !encryptionMatches {
			return nil, fmt.Errorf("resume token `rawok`=%v and `compressok`=%v are incompatible with encryption policy=%v", resumeToken.RawOK, resumeToken.CompressOK, fs.policy.EncryptedSend)
		} else if toVersion == nil {
			return nil, fmt.Errorf("resume token `toguid` = %v not found on sender (`toname` = %q)", resumeToken.ToGUID, resumeToken.ToName)
		} else if fromVersion == toVersion {
			return nil, fmt.Errorf("resume token `fromguid` and `toguid` match same version on sener")
		}
		// fromVersion may be nil, toVersion is no nil, encryption matches
		// good to go this one step!
		resumeStep := &Step{
			parent:   fs,
			sender:   fs.sender,
			receiver: fs.receiver,

			from:    fromVersion,
			to:      toVersion,
			encrypt: fs.policy.EncryptedSend,

			resumeToken: resumeTokenRaw,
		}

		// by definition, the resume token _must_ be the receiver's most recent version, if they have any
		// don't bother checking, zfs recv will produce an error if above assumption is wrong
		//
		// thus, subsequent steps are just incrementals on the sender's remaining _snapshots_ (not bookmarks)

		var remainingSFSVs []*pdu.FilesystemVersion
		for _, sfsv := range sfsvs[toVersionIdx:] {
			if sfsv.Type == pdu.FilesystemVersion_Snapshot {
				remainingSFSVs = append(remainingSFSVs, sfsv)
			}
		}

		steps = make([]*Step, 0, len(remainingSFSVs)) // shadow
		steps = append(steps, resumeStep)
		for i := 0; i < len(remainingSFSVs)-1; i++ {
			steps = append(steps, &Step{
				parent:   fs,
				sender:   fs.sender,
				receiver: fs.receiver,
				from:     remainingSFSVs[i],
				to:       remainingSFSVs[i+1],
				encrypt:  fs.policy.EncryptedSend,
			})
		}
	} else { // resumeToken == nil
		path, conflict := IncrementalPath(rfsvs, sfsvs)
		if conflict != nil {
			var msg string
			path, msg = resolveConflict(conflict) // no shadowing allowed!
			if path != nil {
				log(ctx).WithField("conflict", conflict).Info("conflict")
				log(ctx).WithField("resolution", msg).Info("automatically resolved")
			} else {
				log(ctx).WithField("conflict", conflict).Error("conflict")
				log(ctx).WithField("problem", msg).Error("cannot resolve conflict")
			}
		}
		if len(path) == 0 {
			return nil, conflict
		}

		steps = make([]*Step, 0, len(path)) // shadow
		if len(path) == 1 {
			steps = append(steps, &Step{
				parent:   fs,
				sender:   fs.sender,
				receiver: fs.receiver,

				from:    nil,
				to:      path[0],
				encrypt: fs.policy.EncryptedSend,
			})
		} else {
			for i := 0; i < len(path)-1; i++ {
				steps = append(steps, &Step{
					parent:   fs,
					sender:   fs.sender,
					receiver: fs.receiver,

					from:    path[i],
					to:      path[i+1],
					encrypt: fs.policy.EncryptedSend,
				})
			}
		}
	}

	if len(steps) == 0 {
		log(ctx).Info("planning determined that no replication steps are required")
	}

	log(ctx).Debug("compute send size estimate")
	errs := make(chan error, len(steps))
	fanOutCtx, fanOutCancel := context.WithCancel(ctx)
	_, fanOutAdd, fanOutWait := trace.WithTaskGroup(fanOutCtx, "compute-size-estimate")
	defer fanOutCancel()
	for _, step := range steps {
		step := step // local copy that is moved into the closure
		fanOutAdd(func(ctx context.Context) {
			// TODO instead of the semaphore, rely on resource-exhaustion signaled by the remote endpoint to limit size-estimate requests
			// Send is handled over rpc/dataconn ATM, which doesn't support the resource exhaustion status codes that gRPC defines
			guard, err := fs.sizeEstimateRequestSem.Acquire(ctx)
			if err != nil {
				fanOutCancel()
				return
			}
			defer guard.Release()

			err = step.updateSizeEstimate(ctx)
			if err != nil {
				log(ctx).WithError(err).WithField("step", step).Error("error computing size estimate")
				fanOutCancel()
			}
			errs <- err
		})
	}
	fanOutWait()
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

	log(ctx).Debug("filesystem planning finished")
	return steps, nil
}

func (s *Step) updateSizeEstimate(ctx context.Context) error {

	log := getLogger(ctx)

	sr := s.buildSendRequest(true)

	log.Debug("initiate dry run send request")
	sres, _, err := s.sender.Send(ctx, sr)
	if err != nil {
		log.WithError(err).Error("dry run send request failed")
		return err
	}
	if sres == nil {
		err := fmt.Errorf("dry run send request returned nil send result")
		log.Error(err.Error())
		return err
	}
	s.expectedSize = sres.GetExpectedSize()
	return nil
}

func (s *Step) buildSendRequest(dryRun bool) (sr *pdu.SendReq) {
	fs := s.parent.Path
	sr = &pdu.SendReq{
		Filesystem:        fs,
		From:              s.from, // may be nil
		To:                s.to,
		Encrypted:         s.encrypt.ToPDU(),
		ResumeToken:       s.resumeToken,
		DryRun:            dryRun,
		ReplicationConfig: s.parent.policy.ReplicationConfig,
	}
	return sr
}

func (s *Step) doReplication(ctx context.Context) error {

	fs := s.parent.Path

	log := getLogger(ctx).WithField("filesystem", fs)
	sr := s.buildSendRequest(false)

	log.Debug("initiate send request")
	sres, stream, err := s.sender.Send(ctx, sr)
	if err != nil {
		log.WithError(err).Error("send request failed")
		return err
	}
	if sres == nil {
		err := fmt.Errorf("send request returned nil send result")
		log.Error(err.Error())
		return err
	}
	if stream == nil {
		err := errors.New("send request did not return a stream, broken endpoint implementation")
		return err
	}
	defer stream.Close()

	// Install a byte counter to track progress + for status report
	byteCountingStream := bytecounter.NewReadCloser(stream)
	s.byteCounterMtx.Lock()
	s.byteCounter = byteCountingStream
	s.byteCounterMtx.Unlock()
	defer func() {
		defer s.byteCounterMtx.Lock().Unlock()
		if s.parent.promBytesReplicated != nil {
			s.parent.promBytesReplicated.Add(float64(s.byteCounter.Count()))
		}
	}()

	rr := &pdu.ReceiveReq{
		Filesystem:        fs,
		To:                sr.GetTo(),
		ClearResumeToken:  !sres.UsedResumeToken,
		ReplicationConfig: s.parent.policy.ReplicationConfig,
	}
	log.Debug("initiate receive request")
	_, err = s.receiver.Receive(ctx, rr, byteCountingStream)
	if err != nil {
		log.
			WithError(err).
			WithField("errType", fmt.Sprintf("%T", err)).
			WithField("rr", fmt.Sprintf("%v", rr)).
			Error("receive request failed (might also be error on sender)")
		// This failure could be due to
		// 	- an unexpected exit of ZFS on the sending side
		//  - an unexpected exit of ZFS on the receiving side
		//  - a connectivity issue
		return err
	}
	log.Debug("receive finished")

	log.Debug("tell sender replication completed")
	_, err = s.sender.SendCompleted(ctx, &pdu.SendCompletedReq{
		OriginalReq: sr,
	})
	if err != nil {
		log.WithError(err).Error("error telling sender that replication completed successfully")
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
