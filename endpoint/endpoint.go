// Package endpoint implements replication endpoints for use with package replication.
package endpoint

import (
	"context"
	"fmt"
	"path"
	"sync"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/replication/logic/pdu"
	"github.com/zrepl/zrepl/util/chainlock"
	"github.com/zrepl/zrepl/util/envconst"
	"github.com/zrepl/zrepl/util/semaphore"
	"github.com/zrepl/zrepl/zfs"
)

type SenderConfig struct {
	FSF     zfs.DatasetFilter
	Encrypt *zfs.NilBool
	JobID   JobID
}

func (c *SenderConfig) Validate() error {
	c.JobID.MustValidate()
	if err := c.Encrypt.Validate(); err != nil {
		return errors.Wrap(err, "`Encrypt` field invalid")
	}
	if _, err := StepHoldTag(c.JobID); err != nil {
		return fmt.Errorf("JobID cannot be used for hold tag: %s", err)
	}
	return nil
}

// Sender implements replication.ReplicationEndpoint for a sending side
type Sender struct {
	FSFilter zfs.DatasetFilter
	encrypt  *zfs.NilBool
	jobId    JobID
}

func NewSender(conf SenderConfig) *Sender {
	if err := conf.Validate(); err != nil {
		panic("invalid config" + err.Error())
	}
	return &Sender{
		FSFilter: conf.FSF,
		encrypt:  conf.Encrypt,
		jobId:    conf.JobID,
	}
}

func (s *Sender) filterCheckFS(fs string) (*zfs.DatasetPath, error) {
	dp, err := zfs.NewDatasetPath(fs)
	if err != nil {
		return nil, err
	}
	if dp.Length() == 0 {
		return nil, errors.New("empty filesystem not allowed")
	}
	pass, err := s.FSFilter.Filter(dp)
	if err != nil {
		return nil, err
	}
	if !pass {
		return nil, fmt.Errorf("endpoint does not allow access to filesystem %s", fs)
	}
	return dp, nil
}

func (s *Sender) ListFilesystems(ctx context.Context, r *pdu.ListFilesystemReq) (*pdu.ListFilesystemRes, error) {
	fss, err := zfs.ZFSListMapping(ctx, s.FSFilter)
	if err != nil {
		return nil, err
	}
	rfss := make([]*pdu.Filesystem, len(fss))
	for i := range fss {
		encEnabled, err := zfs.ZFSGetEncryptionEnabled(ctx, fss[i].ToString())
		if err != nil {
			return nil, errors.Wrap(err, "cannot get filesystem encryption status")
		}
		rfss[i] = &pdu.Filesystem{
			Path: fss[i].ToString(),
			// ResumeToken does not make sense from Sender
			IsPlaceholder: false, // sender FSs are never placeholders
			IsEncrypted:   encEnabled,
		}
	}
	res := &pdu.ListFilesystemRes{Filesystems: rfss}
	return res, nil
}

func (s *Sender) ListFilesystemVersions(ctx context.Context, r *pdu.ListFilesystemVersionsReq) (*pdu.ListFilesystemVersionsRes, error) {
	lp, err := s.filterCheckFS(r.GetFilesystem())
	if err != nil {
		return nil, err
	}
	fsvs, err := zfs.ZFSListFilesystemVersions(lp, zfs.ListFilesystemVersionsOptions{})
	if err != nil {
		return nil, err
	}
	rfsvs := make([]*pdu.FilesystemVersion, len(fsvs))
	for i := range fsvs {
		rfsvs[i] = pdu.FilesystemVersionFromZFS(&fsvs[i])
	}
	res := &pdu.ListFilesystemVersionsRes{Versions: rfsvs}
	return res, nil

}

func (p *Sender) HintMostRecentCommonAncestor(ctx context.Context, r *pdu.HintMostRecentCommonAncestorReq) (*pdu.HintMostRecentCommonAncestorRes, error) {

	fsp, err := p.filterCheckFS(r.GetFilesystem())
	if err != nil {
		return nil, err
	}
	fs := fsp.ToString()

	log := getLogger(ctx).WithField("fs", fs).WithField("hinted_most_recent", fmt.Sprintf("%#v", r.GetSenderVersion()))

	log.WithField("full_hint", r).Debug("full hint")

	if r.GetSenderVersion() == nil {
		// no common ancestor found, likely due to failed prior replication attempt
		// => release stale step holds to prevent them from accumulating
		//    (they can accumulate on initial replication because each inital replication step might hold a different `to`)
		// => replication cursors cannot accumulate because we always _move_ the replication cursor
		log.Debug("releasing all step holds on the filesystem")
		TryReleaseStepStaleFS(ctx, fs, p.jobId)
		return &pdu.HintMostRecentCommonAncestorRes{}, nil
	}

	// we were hinted a specific common ancestor

	mostRecentVersion, err := sendArgsFromPDUAndValidateExistsAndGetVersion(ctx, fs, r.GetSenderVersion())
	if err != nil {
		msg := "HintMostRecentCommonAncestor rpc with nonexistent most recent version"
		log.Warn(msg)
		return nil, errors.Wrap(err, msg)
	}

	// move replication cursor to this position
	destroyedCursors, err := MoveReplicationCursor(ctx, fs, mostRecentVersion, p.jobId)
	if err == zfs.ErrBookmarkCloningNotSupported {
		log.Debug("not creating replication cursor from bookmark because ZFS does not support it")
		// fallthrough
	} else if err != nil {
		return nil, errors.Wrap(err, "cannot set replication cursor to hinted version")
	}

	// take care of stale step holds
	log.WithField("step-holds-cleanup-mode", senderHintMostRecentCommonAncestorStepCleanupMode).
		Debug("taking care of possibly stale step holds")
	doStepCleanup := false
	var stepCleanupSince *CreateTXGRangeBound
	switch senderHintMostRecentCommonAncestorStepCleanupMode {
	case StepCleanupNoCleanup:
		doStepCleanup = false
	case StepCleanupRangeSinceUnbounded:
		doStepCleanup = true
		stepCleanupSince = nil
	case StepCleanupRangeSinceReplicationCursor:
		doStepCleanup = true
		// Use the destroyed replication cursors as indicator how far the previous replication got.
		// To be precise: We limit the amount of visisted snapshots to exactly those snapshots
		// created since the last successful replication cursor movement (i.e. last successful replication step)
		//
		// If we crash now, we'll leak the step we are about to release, but the performance gain
		// of limiting the amount of snapshots we visit makes up for that.
		// Users have the `zrepl holds release-stale` command to cleanup leaked step holds.
		for _, destroyed := range destroyedCursors {
			if stepCleanupSince == nil {
				stepCleanupSince = &CreateTXGRangeBound{
					CreateTXG: destroyed.GetCreateTXG(),
					Inclusive: &zfs.NilBool{B: true},
				}
			} else if destroyed.GetCreateTXG() < stepCleanupSince.CreateTXG {
				stepCleanupSince.CreateTXG = destroyed.GetCreateTXG()
			}
		}
	default:
		panic(senderHintMostRecentCommonAncestorStepCleanupMode)
	}
	if !doStepCleanup {
		log.Info("skipping cleanup of prior invocations' step holds due to environment variable setting")
	} else {
		if err := ReleaseStepCummulativeInclusive(ctx, fs, stepCleanupSince, mostRecentVersion, p.jobId); err != nil {
			return nil, errors.Wrap(err, "cannot cleanup prior invocation's step holds and bookmarks")
		} else {
			log.Info("step hold cleanup done")
		}
	}

	return &pdu.HintMostRecentCommonAncestorRes{}, nil
}

type HintMostRecentCommonAncestorStepCleanupMode struct{ string }

var (
	StepCleanupRangeSinceReplicationCursor = HintMostRecentCommonAncestorStepCleanupMode{"range-since-replication-cursor"}
	StepCleanupRangeSinceUnbounded         = HintMostRecentCommonAncestorStepCleanupMode{"range-since-unbounded"}
	StepCleanupNoCleanup                   = HintMostRecentCommonAncestorStepCleanupMode{"no-cleanup"}
)

func (m HintMostRecentCommonAncestorStepCleanupMode) String() string { return string(m.string) }
func (m *HintMostRecentCommonAncestorStepCleanupMode) Set(s string) error {
	switch s {
	case StepCleanupRangeSinceReplicationCursor.String():
		*m = StepCleanupRangeSinceReplicationCursor
	case StepCleanupRangeSinceUnbounded.String():
		*m = StepCleanupRangeSinceUnbounded
	case StepCleanupNoCleanup.String():
		*m = StepCleanupNoCleanup
	default:
		return fmt.Errorf("unknown step cleanup mode %q", s)
	}
	return nil
}

var senderHintMostRecentCommonAncestorStepCleanupMode = *envconst.Var("ZREPL_ENDPOINT_SENDER_HINT_MOST_RECENT_STEP_HOLD_CLEANUP_MODE", &StepCleanupRangeSinceReplicationCursor).(*HintMostRecentCommonAncestorStepCleanupMode)

var maxConcurrentZFSSendSemaphore = semaphore.New(envconst.Int64("ZREPL_ENDPOINT_MAX_CONCURRENT_SEND", 10))

func uncheckedSendArgsFromPDU(fsv *pdu.FilesystemVersion) *zfs.ZFSSendArgVersion {
	if fsv == nil {
		return nil
	}
	return &zfs.ZFSSendArgVersion{RelName: fsv.GetRelName(), GUID: fsv.Guid}
}

func sendArgsFromPDUAndValidateExistsAndGetVersion(ctx context.Context, fs string, fsv *pdu.FilesystemVersion) (v zfs.FilesystemVersion, err error) {
	sendArgs := uncheckedSendArgsFromPDU(fsv)
	if sendArgs == nil {
		return v, errors.New("must not be nil")
	}
	version, err := sendArgs.ValidateExistsAndGetVersion(ctx, fs)
	if err != nil {
		return v, err
	}
	return version, nil
}

func (s *Sender) Send(ctx context.Context, r *pdu.SendReq) (*pdu.SendRes, zfs.StreamCopier, error) {

	_, err := s.filterCheckFS(r.Filesystem)
	if err != nil {
		return nil, nil, err
	}
	switch r.Encrypted {
	case pdu.Tri_DontCare:
		// use s.encrypt setting
		// ok, fallthrough outer
	case pdu.Tri_False:
		if s.encrypt.B {
			return nil, nil, errors.New("only encrypted sends allowed (send -w + encryption!= off), but unencrypted send requested")
		}
		// fallthrough outer
	case pdu.Tri_True:
		if !s.encrypt.B {
			return nil, nil, errors.New("only unencrypted sends allowed, but encrypted send requested")
		}
		// fallthrough outer
	default:
		return nil, nil, fmt.Errorf("unknown pdu.Tri variant %q", r.Encrypted)
	}

	sendArgsUnvalidated := zfs.ZFSSendArgsUnvalidated{
		FS:          r.Filesystem,
		From:        uncheckedSendArgsFromPDU(r.GetFrom()), // validated by zfs.ZFSSendDry / zfs.ZFSSend
		To:          uncheckedSendArgsFromPDU(r.GetTo()),   // validated by zfs.ZFSSendDry / zfs.ZFSSend
		Encrypted:   s.encrypt,
		ResumeToken: r.ResumeToken, // nil or not nil, depending on decoding success
	}

	sendArgs, err := sendArgsUnvalidated.Validate(ctx)
	if err != nil {
		return nil, nil, errors.Wrap(err, "validate send arguments")
	}

	getLogger(ctx).Debug("acquire concurrent send semaphore")
	// TODO use try-acquire and fail with resource-exhaustion rpc status
	// => would require handling on the client-side
	// => this is a dataconn endpoint, doesn't have the status code semantics of gRPC
	guard, err := maxConcurrentZFSSendSemaphore.Acquire(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer guard.Release()

	si, err := zfs.ZFSSendDry(ctx, sendArgs)
	if err != nil {
		return nil, nil, errors.Wrap(err, "zfs send dry failed")
	}

	// From now on, assume that sendArgs has been validated by ZFSSendDry
	// (because validation involves shelling out, it's actually a little expensive)

	var expSize int64 = 0      // protocol says 0 means no estimate
	if si.SizeEstimate != -1 { // but si returns -1 for no size estimate
		expSize = si.SizeEstimate
	}
	res := &pdu.SendRes{
		ExpectedSize:    expSize,
		UsedResumeToken: r.ResumeToken != "",
	}

	if r.DryRun {
		return res, nil, nil
	}

	// update replication cursor
	if sendArgs.From != nil {
		// For all but the first replication, this should always be a no-op because SendCompleted already moved the cursor
		_, err = MoveReplicationCursor(ctx, sendArgs.FS, sendArgs.FromVersion, s.jobId)
		if err == zfs.ErrBookmarkCloningNotSupported {
			getLogger(ctx).Debug("not creating replication cursor from bookmark because ZFS does not support it")
			// fallthrough
		} else if err != nil {
			return nil, nil, errors.Wrap(err, "cannot set replication cursor to `from` version before starting send")
		}
	}

	// make sure `From` doesn't go away in order to make this step resumable
	if sendArgs.From != nil {
		_, err := HoldStep(ctx, sendArgs.FS, *sendArgs.FromVersion, s.jobId)
		if err == zfs.ErrBookmarkCloningNotSupported {
			getLogger(ctx).Debug("not creating step bookmark because ZFS does not support it")
			// fallthrough
		} else if err != nil {
			return nil, nil, errors.Wrapf(err, "cannot hold `from` version %q before starting send", *sendArgs.FromVersion)
		}
	}
	// make sure `To` doesn't go away in order to make this step resumable
	_, err = HoldStep(ctx, sendArgs.FS, sendArgs.ToVersion, s.jobId)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "cannot hold `to` version %q before starting send", sendArgs.ToVersion)
	}

	// step holds & replication cursor released / moved forward in s.SendCompleted => s.moveCursorAndReleaseSendHolds

	streamCopier, err := zfs.ZFSSend(ctx, sendArgs)
	if err != nil {
		return nil, nil, errors.Wrap(err, "zfs send failed")
	}
	return res, streamCopier, nil
}

func (p *Sender) SendCompleted(ctx context.Context, r *pdu.SendCompletedReq) (*pdu.SendCompletedRes, error) {

	orig := r.GetOriginalReq() // may be nil, always use proto getters
	fsp, err := p.filterCheckFS(orig.GetFilesystem())
	if err != nil {
		return nil, err
	}
	fs := fsp.ToString()

	var from *zfs.FilesystemVersion
	if orig.GetFrom() != nil {
		f, err := sendArgsFromPDUAndValidateExistsAndGetVersion(ctx, fs, orig.GetFrom()) // no shadow
		if err != nil {
			return nil, errors.Wrap(err, "validate `from` exists")
		}
		from = &f
	}
	to, err := sendArgsFromPDUAndValidateExistsAndGetVersion(ctx, fs, orig.GetTo())
	if err != nil {
		return nil, errors.Wrap(err, "validate `to` exists")
	}

	log := getLogger(ctx).WithField("to_guid", to.Guid).
		WithField("fs", fs).
		WithField("to", to.RelName)
	if from != nil {
		log = log.WithField("from", from.RelName).WithField("from_guid", from.Guid)
	}

	log.Debug("move replication cursor to most recent common version")
	destroyedCursors, err := MoveReplicationCursor(ctx, fs, to, p.jobId)
	if err != nil {
		if err == zfs.ErrBookmarkCloningNotSupported {
			log.Debug("not setting replication cursor, bookmark cloning not supported")
		} else {
			msg := "cannot move replication cursor, keeping hold on `to` until successful"
			log.WithError(err).Error(msg)
			err = errors.Wrap(err, msg)
			// it is correct to not release the hold if we can't move the cursor!
			return &pdu.SendCompletedRes{}, err
		}
	} else {
		log.Info("successfully moved replication cursor")
	}

	// kick off releasing of step holds / bookmarks
	// if we fail to release them, don't bother the caller:
	// they are merely an implementation detail on the sender for better resumability
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		log.Debug("release step-hold of or step-bookmark on `to`")
		err = ReleaseStep(ctx, fs, to, p.jobId)
		if err != nil {
			log.WithError(err).Error("cannot release step-holds on or destroy step-bookmark of `to`")
		} else {
			log.Info("successfully released step-holds on or destroyed step-bookmark of `to`")
		}

	}()
	go func() {
		defer wg.Done()
		if from == nil {
			return
		}
		log.Debug("release step-hold of or step-bookmark on `from`")
		err := ReleaseStep(ctx, fs, *from, p.jobId)
		if err != nil {
			if dne, ok := err.(*zfs.DatasetDoesNotExist); ok {
				// If bookmark cloning is not supported, `from` might be the old replication cursor
				// and thus have already been destroyed by MoveReplicationCursor above
				// In that case, nonexistence of `from` is not an error, otherwise it is.
				for _, c := range destroyedCursors {
					if c.GetFullPath() == dne.Path {
						log.Info("`from` was a replication cursor and has already been destroyed")
						return
					}
				}
				// fallthrough
			}
			log.WithError(err).Error("cannot release step-holds on or destroy step-bookmark of `from`")
		} else {
			log.Info("successfully released step-holds on or destroyed step-bookmark of `from`")
		}
	}()
	wg.Wait()

	return &pdu.SendCompletedRes{}, nil
}

func (p *Sender) DestroySnapshots(ctx context.Context, req *pdu.DestroySnapshotsReq) (*pdu.DestroySnapshotsRes, error) {
	dp, err := p.filterCheckFS(req.Filesystem)
	if err != nil {
		return nil, err
	}
	return doDestroySnapshots(ctx, dp, req.Snapshots)
}

func (p *Sender) Ping(ctx context.Context, req *pdu.PingReq) (*pdu.PingRes, error) {
	res := pdu.PingRes{
		Echo: req.GetMessage(),
	}
	return &res, nil
}

func (p *Sender) PingDataconn(ctx context.Context, req *pdu.PingReq) (*pdu.PingRes, error) {
	return p.Ping(ctx, req)
}

func (p *Sender) WaitForConnectivity(ctx context.Context) error {
	return nil
}

func (p *Sender) ReplicationCursor(ctx context.Context, req *pdu.ReplicationCursorReq) (*pdu.ReplicationCursorRes, error) {
	dp, err := p.filterCheckFS(req.Filesystem)
	if err != nil {
		return nil, err
	}

	cursor, err := GetMostRecentReplicationCursorOfJob(ctx, dp.ToString(), p.jobId)
	if err != nil {
		return nil, err
	}
	if cursor == nil {
		return &pdu.ReplicationCursorRes{Result: &pdu.ReplicationCursorRes_Notexist{Notexist: true}}, nil
	}
	return &pdu.ReplicationCursorRes{Result: &pdu.ReplicationCursorRes_Guid{Guid: cursor.Guid}}, nil
}

func (p *Sender) Receive(ctx context.Context, r *pdu.ReceiveReq, receive zfs.StreamCopier) (*pdu.ReceiveRes, error) {
	return nil, fmt.Errorf("sender does not implement Receive()")
}

type FSFilter interface { // FIXME unused
	Filter(path *zfs.DatasetPath) (pass bool, err error)
}

// FIXME: can we get away without error types here?
type FSMap interface { // FIXME unused
	FSFilter
	Map(path *zfs.DatasetPath) (*zfs.DatasetPath, error)
	Invert() (FSMap, error)
	AsFilter() FSFilter
}

type ReceiverConfig struct {
	JobID JobID

	RootWithoutClientComponent *zfs.DatasetPath // TODO use
	AppendClientIdentity       bool

	UpdateLastReceivedHold bool
}

func (c *ReceiverConfig) copyIn() {
	c.RootWithoutClientComponent = c.RootWithoutClientComponent.Copy()
}

func (c *ReceiverConfig) Validate() error {
	c.JobID.MustValidate()
	if c.RootWithoutClientComponent.Length() <= 0 {
		return errors.New("RootWithoutClientComponent must not be an empty dataset path")
	}
	return nil
}

// Receiver implements replication.ReplicationEndpoint for a receiving side
type Receiver struct {
	conf ReceiverConfig // validated

	recvParentCreationMtx *chainlock.L
}

func NewReceiver(config ReceiverConfig) *Receiver {
	config.copyIn()
	if err := config.Validate(); err != nil {
		panic(err)
	}
	return &Receiver{
		conf:                  config,
		recvParentCreationMtx: chainlock.New(),
	}
}

func TestClientIdentity(rootFS *zfs.DatasetPath, clientIdentity string) error {
	_, err := clientRoot(rootFS, clientIdentity)
	return err
}

func clientRoot(rootFS *zfs.DatasetPath, clientIdentity string) (*zfs.DatasetPath, error) {
	rootFSLen := rootFS.Length()
	clientRootStr := path.Join(rootFS.ToString(), clientIdentity)
	clientRoot, err := zfs.NewDatasetPath(clientRootStr)
	if err != nil {
		return nil, err
	}
	if rootFSLen+1 != clientRoot.Length() {
		return nil, fmt.Errorf("client identity must be a single ZFS filesystem path component")
	}
	return clientRoot, nil
}

func (s *Receiver) clientRootFromCtx(ctx context.Context) *zfs.DatasetPath {
	if !s.conf.AppendClientIdentity {
		return s.conf.RootWithoutClientComponent.Copy()
	}

	clientIdentity, ok := ctx.Value(ClientIdentityKey).(string)
	if !ok {
		panic(fmt.Sprintf("ClientIdentityKey context value must be set"))
	}

	clientRoot, err := clientRoot(s.conf.RootWithoutClientComponent, clientIdentity)
	if err != nil {
		panic(fmt.Sprintf("ClientIdentityContextKey must have been validated before invoking Receiver: %s", err))
	}
	return clientRoot
}

type subroot struct {
	localRoot *zfs.DatasetPath
}

var _ zfs.DatasetFilter = subroot{}

// Filters local p
func (f subroot) Filter(p *zfs.DatasetPath) (pass bool, err error) {
	return p.HasPrefix(f.localRoot) && !p.Equal(f.localRoot), nil
}

func (f subroot) MapToLocal(fs string) (*zfs.DatasetPath, error) {
	p, err := zfs.NewDatasetPath(fs)
	if err != nil {
		return nil, err
	}
	if p.Length() == 0 {
		return nil, errors.Errorf("cannot map empty filesystem")
	}
	c := f.localRoot.Copy()
	c.Extend(p)
	return c, nil
}

func (s *Receiver) ListFilesystems(ctx context.Context, req *pdu.ListFilesystemReq) (*pdu.ListFilesystemRes, error) {
	root := s.clientRootFromCtx(ctx)
	filtered, err := zfs.ZFSListMapping(ctx, subroot{root})
	if err != nil {
		return nil, err
	}
	// present filesystem without the root_fs prefix
	fss := make([]*pdu.Filesystem, 0, len(filtered))
	for _, a := range filtered {
		l := getLogger(ctx).WithField("fs", a)
		ph, err := zfs.ZFSGetFilesystemPlaceholderState(ctx, a)
		if err != nil {
			l.WithError(err).Error("error getting placeholder state")
			return nil, errors.Wrapf(err, "cannot get placeholder state for fs %q", a)
		}
		l.WithField("placeholder_state", fmt.Sprintf("%#v", ph)).Debug("placeholder state")
		if !ph.FSExists {
			l.Error("inconsistent placeholder state: filesystem must exists")
			err := errors.Errorf("inconsistent placeholder state: filesystem %q must exist in this context", a.ToString())
			return nil, err
		}
		token, err := zfs.ZFSGetReceiveResumeTokenOrEmptyStringIfNotSupported(ctx, a)
		if err != nil {
			l.WithError(err).Error("cannot get receive resume token")
			return nil, err
		}
		encEnabled, err := zfs.ZFSGetEncryptionEnabled(ctx, a.ToString())
		if err != nil {
			l.WithError(err).Error("cannot get encryption enabled status")
			return nil, err
		}
		l.WithField("receive_resume_token", token).Debug("receive resume token")

		a.TrimPrefix(root)

		fs := &pdu.Filesystem{
			Path:          a.ToString(),
			IsPlaceholder: ph.IsPlaceholder,
			ResumeToken:   token,
			IsEncrypted:   encEnabled,
		}
		fss = append(fss, fs)
	}
	if len(fss) == 0 {
		getLogger(ctx).Debug("no filesystems found")
		return &pdu.ListFilesystemRes{}, nil
	}
	return &pdu.ListFilesystemRes{Filesystems: fss}, nil
}

func (s *Receiver) ListFilesystemVersions(ctx context.Context, req *pdu.ListFilesystemVersionsReq) (*pdu.ListFilesystemVersionsRes, error) {
	root := s.clientRootFromCtx(ctx)
	lp, err := subroot{root}.MapToLocal(req.GetFilesystem())
	if err != nil {
		return nil, err
	}
	// TODO share following code with sender

	fsvs, err := zfs.ZFSListFilesystemVersions(lp, zfs.ListFilesystemVersionsOptions{})
	if err != nil {
		return nil, err
	}

	rfsvs := make([]*pdu.FilesystemVersion, len(fsvs))
	for i := range fsvs {
		rfsvs[i] = pdu.FilesystemVersionFromZFS(&fsvs[i])
	}

	return &pdu.ListFilesystemVersionsRes{Versions: rfsvs}, nil
}

func (s *Receiver) Ping(ctx context.Context, req *pdu.PingReq) (*pdu.PingRes, error) {
	res := pdu.PingRes{
		Echo: req.GetMessage(),
	}
	return &res, nil
}

func (s *Receiver) PingDataconn(ctx context.Context, req *pdu.PingReq) (*pdu.PingRes, error) {
	return s.Ping(ctx, req)
}

func (s *Receiver) WaitForConnectivity(ctx context.Context) error {
	return nil
}

func (s *Receiver) ReplicationCursor(context.Context, *pdu.ReplicationCursorReq) (*pdu.ReplicationCursorRes, error) {
	return nil, fmt.Errorf("ReplicationCursor not implemented for Receiver")
}

func (s *Receiver) Send(ctx context.Context, req *pdu.SendReq) (*pdu.SendRes, zfs.StreamCopier, error) {
	return nil, nil, fmt.Errorf("receiver does not implement Send()")
}

var maxConcurrentZFSRecvSemaphore = semaphore.New(envconst.Int64("ZREPL_ENDPOINT_MAX_CONCURRENT_RECV", 10))

func (s *Receiver) Receive(ctx context.Context, req *pdu.ReceiveReq, receive zfs.StreamCopier) (*pdu.ReceiveRes, error) {
	getLogger(ctx).Debug("incoming Receive")
	defer receive.Close()

	root := s.clientRootFromCtx(ctx)
	lp, err := subroot{root}.MapToLocal(req.Filesystem)
	if err != nil {
		return nil, errors.Wrap(err, "`Filesystem` invalid")
	}

	to := uncheckedSendArgsFromPDU(req.GetTo())
	if to == nil {
		return nil, errors.New("`To` must not be nil")
	}
	if !to.IsSnapshot() {
		return nil, errors.New("`To` must be a snapshot")
	}

	// create placeholder parent filesystems as appropriate
	//
	// Manipulating the ZFS dataset hierarchy must happen exclusively.
	// TODO: Use fine-grained locking to allow separate clients / requests to pass
	// 		 through the following section concurrently when operating on disjoint
	//       ZFS dataset hierarchy subtrees.
	var visitErr error
	func() {
		getLogger(ctx).Debug("begin acquire recvParentCreationMtx")
		defer s.recvParentCreationMtx.Lock().Unlock()
		getLogger(ctx).Debug("end acquire recvParentCreationMtx")
		defer getLogger(ctx).Debug("release recvParentCreationMtx")

		f := zfs.NewDatasetPathForest()
		f.Add(lp)
		getLogger(ctx).Debug("begin tree-walk")
		f.WalkTopDown(func(v zfs.DatasetPathVisit) (visitChildTree bool) {
			if v.Path.Equal(lp) {
				return false
			}
			ph, err := zfs.ZFSGetFilesystemPlaceholderState(ctx, v.Path)
			getLogger(ctx).
				WithField("fs", v.Path.ToString()).
				WithField("placeholder_state", fmt.Sprintf("%#v", ph)).
				WithField("err", fmt.Sprintf("%s", err)).
				WithField("errType", fmt.Sprintf("%T", err)).
				Debug("placeholder state for filesystem")
			if err != nil {
				visitErr = err
				return false
			}

			if !ph.FSExists {
				if s.conf.RootWithoutClientComponent.HasPrefix(v.Path) {
					if v.Path.Length() == 1 {
						visitErr = fmt.Errorf("pool %q not imported", v.Path.ToString())
					} else {
						visitErr = fmt.Errorf("root_fs %q does not exist", s.conf.RootWithoutClientComponent.ToString())
					}
					getLogger(ctx).WithError(visitErr).Error("placeholders are only created automatically below root_fs")
					return false
				}
				l := getLogger(ctx).WithField("placeholder_fs", v.Path)
				l.Debug("create placeholder filesystem")
				err := zfs.ZFSCreatePlaceholderFilesystem(ctx, v.Path)
				if err != nil {
					l.WithError(err).Error("cannot create placeholder filesystem")
					visitErr = err
					return false
				}
				return true
			}
			getLogger(ctx).WithField("filesystem", v.Path.ToString()).Debug("exists")
			return true // leave this fs as is
		})
	}()
	getLogger(ctx).WithField("visitErr", visitErr).Debug("complete tree-walk")
	if visitErr != nil {
		return nil, visitErr
	}

	// determine whether we need to rollback the filesystem / change its placeholder state
	var clearPlaceholderProperty bool
	var recvOpts zfs.RecvOptions
	ph, err := zfs.ZFSGetFilesystemPlaceholderState(ctx, lp)
	if err == nil && ph.FSExists && ph.IsPlaceholder {
		recvOpts.RollbackAndForceRecv = true
		clearPlaceholderProperty = true
	}
	if clearPlaceholderProperty {
		if err := zfs.ZFSSetPlaceholder(ctx, lp, false); err != nil {
			return nil, fmt.Errorf("cannot clear placeholder property for forced receive: %s", err)
		}
	}

	if req.ClearResumeToken && ph.FSExists {
		if err := zfs.ZFSRecvClearResumeToken(ctx, lp.ToString()); err != nil {
			return nil, errors.Wrap(err, "cannot clear resume token")
		}
	}

	recvOpts.SavePartialRecvState, err = zfs.ResumeRecvSupported(ctx, lp)
	if err != nil {
		return nil, errors.Wrap(err, "cannot determine whether we can use resumable send & recv")
	}

	getLogger(ctx).Debug("acquire concurrent recv semaphore")
	// TODO use try-acquire and fail with resource-exhaustion rpc status
	// => would require handling on the client-side
	// => this is a dataconn endpoint, doesn't have the status code semantics of gRPC
	guard, err := maxConcurrentZFSRecvSemaphore.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer guard.Release()

	getLogger(ctx).WithField("opts", fmt.Sprintf("%#v", recvOpts)).Debug("start receive command")

	snapFullPath := to.FullPath(lp.ToString())
	if err := zfs.ZFSRecv(ctx, lp.ToString(), to, receive, recvOpts); err != nil {
		getLogger(ctx).
			WithError(err).
			WithField("opts", fmt.Sprintf("%#v", recvOpts)).
			Error("zfs receive failed")
		return nil, err
	}

	// validate that we actually received what the sender claimed
	toRecvd, err := to.ValidateExistsAndGetVersion(ctx, lp.ToString())
	if err != nil {
		msg := "receive request's `To` version does not match what we received in the stream"
		getLogger(ctx).WithError(err).WithField("snap", snapFullPath).Error(msg)
		getLogger(ctx).Error("aborting recv request, but keeping received snapshot for inspection")
		return nil, errors.Wrap(err, msg)
	}

	if s.conf.UpdateLastReceivedHold {
		getLogger(ctx).Debug("move last-received-hold")
		if err := MoveLastReceivedHold(ctx, lp.ToString(), toRecvd, s.conf.JobID); err != nil {
			return nil, errors.Wrap(err, "cannot move last-received-hold")
		}
	}

	return &pdu.ReceiveRes{}, nil
}

func (s *Receiver) DestroySnapshots(ctx context.Context, req *pdu.DestroySnapshotsReq) (*pdu.DestroySnapshotsRes, error) {
	root := s.clientRootFromCtx(ctx)
	lp, err := subroot{root}.MapToLocal(req.Filesystem)
	if err != nil {
		return nil, err
	}
	return doDestroySnapshots(ctx, lp, req.Snapshots)
}

func (p *Receiver) HintMostRecentCommonAncestor(ctx context.Context, r *pdu.HintMostRecentCommonAncestorReq) (*pdu.HintMostRecentCommonAncestorRes, error) {
	// we don't move last-received-hold as part of this hint
	// because that wouldn't give us any benefit wrt resumability.
	//
	// Other reason: the replication logic that issues this RPC would require refactoring
	// to include the receiver's FilesystemVersion in the request)
	return &pdu.HintMostRecentCommonAncestorRes{}, nil
}

func (p *Receiver) SendCompleted(context.Context, *pdu.SendCompletedReq) (*pdu.SendCompletedRes, error) {
	return &pdu.SendCompletedRes{}, nil
}

func doDestroySnapshots(ctx context.Context, lp *zfs.DatasetPath, snaps []*pdu.FilesystemVersion) (*pdu.DestroySnapshotsRes, error) {
	reqs := make([]*zfs.DestroySnapOp, len(snaps))
	ress := make([]*pdu.DestroySnapshotRes, len(snaps))
	errs := make([]error, len(snaps))
	for i, fsv := range snaps {
		if fsv.Type != pdu.FilesystemVersion_Snapshot {
			return nil, fmt.Errorf("version %q is not a snapshot", fsv.Name)
		}
		ress[i] = &pdu.DestroySnapshotRes{
			Snapshot: fsv,
			// Error set after batch operation
		}
		reqs[i] = &zfs.DestroySnapOp{
			Filesystem: lp.ToString(),
			Name:       fsv.Name,
			ErrOut:     &errs[i],
		}
	}
	zfs.ZFSDestroyFilesystemVersions(reqs)
	for i := range reqs {
		if errs[i] != nil {
			if de, ok := errs[i].(*zfs.DestroySnapshotsError); ok && len(de.Reason) == 1 {
				ress[i].Error = de.Reason[0]
			} else {
				ress[i].Error = errs[i].Error()
			}
		}
	}
	return &pdu.DestroySnapshotsRes{
		Results: ress,
	}, nil
}
