// Package endpoint implements replication endpoints for use with package replication.
package endpoint

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path"

	"github.com/kr/pretty"
	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/daemon/logging/trace"

	"github.com/zrepl/zrepl/replication/logic/pdu"
	"github.com/zrepl/zrepl/util/chainedio"
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
	pdu.UnsafeReplicationServer // prefer compilation errors over default 'method X not implemented' impl

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
	defer trace.WithSpanFromStackUpdateCtx(&ctx)()

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
	defer trace.WithSpanFromStackUpdateCtx(&ctx)()

	lp, err := s.filterCheckFS(r.GetFilesystem())
	if err != nil {
		return nil, err
	}
	fsvs, err := zfs.ZFSListFilesystemVersions(ctx, lp, zfs.ListFilesystemVersionsOptions{})
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

var maxConcurrentZFSSend = envconst.Int64("ZREPL_ENDPOINT_MAX_CONCURRENT_SEND", 10)
var maxConcurrentZFSSendSemaphore = semaphore.New(maxConcurrentZFSSend)

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

func (s *Sender) Send(ctx context.Context, r *pdu.SendReq) (*pdu.SendRes, io.ReadCloser, error) {
	defer trace.WithSpanFromStackUpdateCtx(&ctx)()

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

	// create holds or bookmarks of `From` and `To` to guarantee one of the following:
	// - that the replication step can always be resumed (`holds`),
	// - that the replication step can be interrupted and a future replication
	//   step with same or different `To` but same `From` is still possible (`bookmarks`)
	// - nothing (`none`)
	//
	// ...
	//
	// ... actually create the abstractions
	replicationGuaranteeOptions, err := replicationGuaranteeOptionsFromPDU(r.GetReplicationConfig().Protection)
	if err != nil {
		return nil, nil, err
	}
	replicationGuaranteeStrategy := replicationGuaranteeOptions.Strategy(sendArgs.From != nil)
	liveAbs, err := replicationGuaranteeStrategy.SenderPreSend(ctx, s.jobId, &sendArgs)
	if err != nil {
		return nil, nil, err
	}
	for _, a := range liveAbs {
		if a != nil {
			abstractionsCacheSingleton.Put(a)
		}
	}

	// cleanup the mess that _this function_ might have created in prior failed attempts:
	//
	// In summary, we delete every endpoint ZFS abstraction created on this filesystem for this job id,
	// except for the ones we just created above.
	//
	// This is the most robust approach to avoid leaking (= forgetting to clean up) endpoint ZFS abstractions,
	// all under the assumption that there will only ever be one send for a (jobId,fs) combination at any given time.
	//
	// Note that the SendCompleted rpc can't be relied upon for this purpose:
	// - it might be lost due to network errors,
	// - or never be sent by a potentially malicious or buggy client,
	// - or never be send because the replication step failed at some point
	//   (potentially leaving a resumable state on the receiver, which is the case where we really do not want to blow away the step holds too soon.)
	//
	// Note further that a resuming send, due to the idempotent nature of func CreateReplicationCursor and HoldStep,
	// will never lose its step holds because we just (idempotently re-)created them above, before attempting the cleanup.
	func() {
		ctx, endSpan := trace.WithSpan(ctx, "cleanup-stale-abstractions")
		defer endSpan()

		keep := func(a Abstraction) (keep bool) {
			keep = false
			for _, k := range liveAbs {
				keep = keep || AbstractionEquals(a, k)
			}
			return keep
		}
		check := func(obsoleteAbs []Abstraction) {
			// last line of defense: check that we don't destroy the incremental `from` and `to`
			// if we did that, we might be about to blow away the last common filesystem version between sender and receiver
			mustLiveVersions := []zfs.FilesystemVersion{sendArgs.ToVersion}
			if sendArgs.FromVersion != nil {
				mustLiveVersions = append(mustLiveVersions, *sendArgs.FromVersion)
			}
			for _, staleVersion := range obsoleteAbs {
				for _, mustLiveVersion := range mustLiveVersions {
					isSendArg := zfs.FilesystemVersionEqualIdentity(mustLiveVersion, staleVersion.GetFilesystemVersion())
					stepHoldBasedGuaranteeStrategy := false
					k := replicationGuaranteeStrategy.Kind()
					switch k {
					case ReplicationGuaranteeKindResumability:
						stepHoldBasedGuaranteeStrategy = true
					case ReplicationGuaranteeKindIncremental:
					case ReplicationGuaranteeKindNone:
					default:
						panic(fmt.Sprintf("this is supposed to be an exhaustive match, got %v", k))
					}
					isSnapshot := mustLiveVersion.IsSnapshot()
					if isSendArg && (!isSnapshot || stepHoldBasedGuaranteeStrategy) {
						panic(fmt.Sprintf("impl error: %q would be destroyed because it is considered stale but it is part of of sendArgs=%s", mustLiveVersion.String(), pretty.Sprint(sendArgs)))
					}
				}
			}
		}
		destroyTypes := AbstractionTypeSet{
			AbstractionStepHold:                           true,
			AbstractionTentativeReplicationCursorBookmark: true,
		}
		abstractionsCacheSingleton.TryBatchDestroy(ctx, s.jobId, sendArgs.FS, destroyTypes, keep, check)
	}()

	sendStream, err := zfs.ZFSSend(ctx, sendArgs)
	if err != nil {
		// it's ok to not destroy the abstractions we just created here, a new send attempt will take care of it
		return nil, nil, errors.Wrap(err, "zfs send failed")
	}

	return res, sendStream, nil
}

func (p *Sender) SendCompleted(ctx context.Context, r *pdu.SendCompletedReq) (*pdu.SendCompletedRes, error) {
	defer trace.WithSpanFromStackUpdateCtx(&ctx)()

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

	replicationGuaranteeOptions, err := replicationGuaranteeOptionsFromPDU(orig.GetReplicationConfig().Protection)
	if err != nil {
		return nil, err
	}
	liveAbs, err := replicationGuaranteeOptions.Strategy(from != nil).SenderPostRecvConfirmed(ctx, p.jobId, fs, to)
	if err != nil {
		return nil, err
	}
	for _, a := range liveAbs {
		if a != nil {
			abstractionsCacheSingleton.Put(a)
		}
	}
	keep := func(a Abstraction) (keep bool) {
		keep = false
		for _, k := range liveAbs {
			keep = keep || AbstractionEquals(a, k)
		}
		return keep
	}
	destroyTypes := AbstractionTypeSet{
		AbstractionStepHold:                           true,
		AbstractionTentativeReplicationCursorBookmark: true,
		AbstractionReplicationCursorBookmarkV2:        true,
	}
	abstractionsCacheSingleton.TryBatchDestroy(ctx, p.jobId, fs, destroyTypes, keep, nil)

	return &pdu.SendCompletedRes{}, nil

}

func (p *Sender) DestroySnapshots(ctx context.Context, req *pdu.DestroySnapshotsReq) (*pdu.DestroySnapshotsRes, error) {
	defer trace.WithSpanFromStackUpdateCtx(&ctx)()

	dp, err := p.filterCheckFS(req.Filesystem)
	if err != nil {
		return nil, err
	}
	return doDestroySnapshots(ctx, dp, req.Snapshots)
}

func (p *Sender) Ping(ctx context.Context, req *pdu.PingReq) (*pdu.PingRes, error) {
	defer trace.WithSpanFromStackUpdateCtx(&ctx)()

	res := pdu.PingRes{
		Echo: req.GetMessage(),
	}
	return &res, nil
}

func (p *Sender) PingDataconn(ctx context.Context, req *pdu.PingReq) (*pdu.PingRes, error) {
	defer trace.WithSpanFromStackUpdateCtx(&ctx)()

	return p.Ping(ctx, req)
}

func (p *Sender) WaitForConnectivity(ctx context.Context) error {
	defer trace.WithSpanFromStackUpdateCtx(&ctx)()

	return nil
}

func (p *Sender) ReplicationCursor(ctx context.Context, req *pdu.ReplicationCursorReq) (*pdu.ReplicationCursorRes, error) {
	defer trace.WithSpanFromStackUpdateCtx(&ctx)()

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

func (p *Sender) Receive(ctx context.Context, r *pdu.ReceiveReq, _ io.ReadCloser) (*pdu.ReceiveRes, error) {
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
	pdu.UnsafeReplicationServer // prefer compilation errors over default 'method X not implemented' impl

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
		panic("ClientIdentityKey context value must be set")
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
	defer trace.WithSpanFromStackUpdateCtx(&ctx)()

	// first make sure that root_fs is imported
	if rphs, err := zfs.ZFSGetFilesystemPlaceholderState(ctx, s.conf.RootWithoutClientComponent); err != nil {
		return nil, errors.Wrap(err, "cannot determine whether root_fs exists")
	} else if !rphs.FSExists {
		return nil, errors.New("root_fs does not exist")
	}

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
	defer trace.WithSpanFromStackUpdateCtx(&ctx)()

	root := s.clientRootFromCtx(ctx)
	lp, err := subroot{root}.MapToLocal(req.GetFilesystem())
	if err != nil {
		return nil, err
	}
	// TODO share following code with sender

	fsvs, err := zfs.ZFSListFilesystemVersions(ctx, lp, zfs.ListFilesystemVersionsOptions{})
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
	defer trace.WithSpanFromStackUpdateCtx(&ctx)()

	res := pdu.PingRes{
		Echo: req.GetMessage(),
	}
	return &res, nil
}

func (s *Receiver) PingDataconn(ctx context.Context, req *pdu.PingReq) (*pdu.PingRes, error) {
	defer trace.WithSpanFromStackUpdateCtx(&ctx)()
	return s.Ping(ctx, req)
}

func (s *Receiver) WaitForConnectivity(ctx context.Context) error {
	defer trace.WithSpanFromStackUpdateCtx(&ctx)()
	return nil
}

func (s *Receiver) ReplicationCursor(ctx context.Context, _ *pdu.ReplicationCursorReq) (*pdu.ReplicationCursorRes, error) {
	defer trace.WithSpanFromStackUpdateCtx(&ctx)()
	return nil, fmt.Errorf("ReplicationCursor not implemented for Receiver")
}

func (s *Receiver) Send(ctx context.Context, req *pdu.SendReq) (*pdu.SendRes, io.ReadCloser, error) {
	defer trace.WithSpanFromStackUpdateCtx(&ctx)()
	return nil, nil, fmt.Errorf("receiver does not implement Send()")
}

var maxConcurrentZFSRecvSemaphore = semaphore.New(envconst.Int64("ZREPL_ENDPOINT_MAX_CONCURRENT_RECV", 10))

func (s *Receiver) Receive(ctx context.Context, req *pdu.ReceiveReq, receive io.ReadCloser) (*pdu.ReceiveRes, error) {
	defer trace.WithSpanFromStackUpdateCtx(&ctx)()

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
		f.WalkTopDown(func(v *zfs.DatasetPathVisit) (visitChildTree bool) {
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
				err := zfs.ZFSCreatePlaceholderFilesystem(ctx, v.Path, v.Parent.Path)
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

	log := getLogger(ctx).WithField("proto_fs", req.GetFilesystem()).WithField("local_fs", lp.ToString())

	// determine whether we need to rollback the filesystem / change its placeholder state
	var clearPlaceholderProperty bool
	var recvOpts zfs.RecvOptions
	ph, err := zfs.ZFSGetFilesystemPlaceholderState(ctx, lp)
	if err != nil {
		return nil, errors.Wrap(err, "cannot get placeholder state")
	}
	log.WithField("placeholder_state", fmt.Sprintf("%#v", ph)).Debug("placeholder state")
	if ph.FSExists && ph.IsPlaceholder {
		recvOpts.RollbackAndForceRecv = true
		clearPlaceholderProperty = true
	}

	if clearPlaceholderProperty {
		log.Info("clearing placeholder property")
		if err := zfs.ZFSSetPlaceholder(ctx, lp, false); err != nil {
			return nil, fmt.Errorf("cannot clear placeholder property for forced receive: %s", err)
		}
	}

	if req.ClearResumeToken && ph.FSExists {
		log.Info("clearing resume token")
		if err := zfs.ZFSRecvClearResumeToken(ctx, lp.ToString()); err != nil {
			return nil, errors.Wrap(err, "cannot clear resume token")
		}
	}

	recvOpts.SavePartialRecvState, err = zfs.ResumeRecvSupported(ctx, lp)
	if err != nil {
		return nil, errors.Wrap(err, "cannot determine whether we can use resumable send & recv")
	}

	log.Debug("acquire concurrent recv semaphore")
	// TODO use try-acquire and fail with resource-exhaustion rpc status
	// => would require handling on the client-side
	// => this is a dataconn endpoint, doesn't have the status code semantics of gRPC
	guard, err := maxConcurrentZFSRecvSemaphore.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer guard.Release()

	var peek bytes.Buffer
	var MaxPeek = envconst.Int64("ZREPL_ENDPOINT_RECV_PEEK_SIZE", 1<<20)
	log.WithField("max_peek_bytes", MaxPeek).Info("peeking incoming stream")
	if _, err := io.Copy(&peek, io.LimitReader(receive, MaxPeek)); err != nil {
		log.WithError(err).Error("cannot read peek-buffer from send stream")
	}
	var peekCopy bytes.Buffer
	if n, err := peekCopy.Write(peek.Bytes()); err != nil || n != peek.Len() {
		panic(peek.Len())
	}

	log.WithField("opts", fmt.Sprintf("%#v", recvOpts)).Debug("start receive command")

	snapFullPath := to.FullPath(lp.ToString())
	if err := zfs.ZFSRecv(ctx, lp.ToString(), to, chainedio.NewChainedReader(&peek, receive), recvOpts); err != nil {

		// best-effort rollback of placeholder state if the recv didn't start
		_, resumableStatePresent := err.(*zfs.RecvFailedWithResumeTokenErr)
		disablePlaceholderRestoration := envconst.Bool("ZREPL_ENDPOINT_DISABLE_PLACEHOLDER_RESTORATION", false)
		placeholderRestored := !ph.IsPlaceholder
		if !disablePlaceholderRestoration && !resumableStatePresent && recvOpts.RollbackAndForceRecv && ph.FSExists && ph.IsPlaceholder && clearPlaceholderProperty {
			log.Info("restoring placeholder property")
			if phErr := zfs.ZFSSetPlaceholder(ctx, lp, true); phErr != nil {
				log.WithError(phErr).Error("cannot restore placeholder property after failed receive, subsequent replications will likely fail with a different error")
				// fallthrough
			} else {
				placeholderRestored = true
			}
			// fallthrough
		}

		// deal with failing initial encrypted send & recv
		if _, ok := err.(*zfs.RecvDestroyOrOverwriteEncryptedErr); ok && ph.IsPlaceholder && placeholderRestored {
			msg := `cannot automatically replace placeholder filesystem with incoming send stream - please see receive-side log for details`
			err := errors.New(msg)
			log.Error(msg)

			log.Error(`zrepl creates placeholder filesystems on the receiving side of a replication to match the sending side's dataset hierarchy`)
			log.Error(`zrepl uses zfs receive -F to replace those placeholders with incoming full sends`)
			log.Error(`OpenZFS native encryption prohibits zfs receive -F for encrypted filesystems`)
			log.Error(`the current zrepl placeholder filesystem concept is thus incompatible with OpenZFS native encryption`)

			tempStartFullRecvFS := lp.Copy().ToString() + ".zrepl.initial-recv"
			tempStartFullRecvFSDP, dpErr := zfs.NewDatasetPath(tempStartFullRecvFS)
			if dpErr != nil {
				log.WithError(dpErr).Error("cannot determine temporary filesystem name for initial encrypted recv workaround")
				return nil, err // yes, err, not dpErr
			}

			log := log.WithField("temp_recv_fs", tempStartFullRecvFS)
			log.Error(`as a workaround, zrepl will now attempt to re-receive the beginning of the stream into a temporary filesystem temp_recv_fs`)
			log.Error(`if that step succeeds: shut down zrepl and use 'zfs rename' to swap temp_recv_fs with local_fs, then restart zrepl`)
			log.Error(`replication will then resume using resumable send+recv`)

			tempPH, phErr := zfs.ZFSGetFilesystemPlaceholderState(ctx, tempStartFullRecvFSDP)
			if phErr != nil {
				log.WithError(phErr).Error("cannot determine placeholder state of temp_recv_fs")
				return nil, err // yes, err, not dpErr
			}
			if tempPH.FSExists {
				log.Error("temp_recv_fs already exists, assuming a (partial) initial recv to that filesystem has already been done")
				return nil, err
			}

			recvOpts.RollbackAndForceRecv = false
			recvOpts.SavePartialRecvState = true
			rerecvErr := zfs.ZFSRecv(ctx, tempStartFullRecvFS, to, chainedio.NewChainedReader(&peekCopy), recvOpts)
			if _, isResumable := rerecvErr.(*zfs.RecvFailedWithResumeTokenErr); rerecvErr == nil || isResumable {
				log.Error("completed re-receive into temporary filesystem temp_recv_fs, now shut down zrepl and use zfs rename to swap temp_recv_fs with local_fs")
			} else {
				log.WithError(rerecvErr).Error("failed to receive the beginning of the stream into temporary filesystem temp_recv_fs")
				log.Error("we advise you to collect the error log and current configuration, open an issue on GitHub, and revert to your previous configuration in the meantime")
			}

			log.Error(`if you would like to see improvements to this situation, please open an issue on GitHub`)
			return nil, err
		}

		log.
			WithError(err).
			WithField("opts", fmt.Sprintf("%#v", recvOpts)).
			Error("zfs receive failed")

		return nil, err
	}

	// validate that we actually received what the sender claimed
	toRecvd, err := to.ValidateExistsAndGetVersion(ctx, lp.ToString())
	if err != nil {
		msg := "receive request's `To` version does not match what we received in the stream"
		log.WithError(err).WithField("snap", snapFullPath).Error(msg)
		log.Error("aborting recv request, but keeping received snapshot for inspection")
		return nil, errors.Wrap(err, msg)
	}

	replicationGuaranteeOptions, err := replicationGuaranteeOptionsFromPDU(req.GetReplicationConfig().Protection)
	if err != nil {
		return nil, err
	}
	replicationGuaranteeStrategy := replicationGuaranteeOptions.Strategy(ph.FSExists)
	liveAbs, err := replicationGuaranteeStrategy.ReceiverPostRecv(ctx, s.conf.JobID, lp.ToString(), toRecvd)
	if err != nil {
		return nil, err
	}
	for _, a := range liveAbs {
		if a != nil {
			abstractionsCacheSingleton.Put(a)
		}
	}
	keep := func(a Abstraction) (keep bool) {
		keep = false
		for _, k := range liveAbs {
			keep = keep || AbstractionEquals(a, k)
		}
		return keep
	}
	check := func(obsoleteAbs []Abstraction) {
		for _, abs := range obsoleteAbs {
			if zfs.FilesystemVersionEqualIdentity(abs.GetFilesystemVersion(), toRecvd) {
				panic(fmt.Sprintf("would destroy endpoint abstraction around the filesystem version we just received %s", abs))
			}
		}
	}
	destroyTypes := AbstractionTypeSet{
		AbstractionLastReceivedHold: true,
	}
	abstractionsCacheSingleton.TryBatchDestroy(ctx, s.conf.JobID, lp.ToString(), destroyTypes, keep, check)

	return &pdu.ReceiveRes{}, nil
}

func (s *Receiver) DestroySnapshots(ctx context.Context, req *pdu.DestroySnapshotsReq) (*pdu.DestroySnapshotsRes, error) {
	defer trace.WithSpanFromStackUpdateCtx(&ctx)()

	root := s.clientRootFromCtx(ctx)
	lp, err := subroot{root}.MapToLocal(req.Filesystem)
	if err != nil {
		return nil, err
	}
	return doDestroySnapshots(ctx, lp, req.Snapshots)
}

func (p *Receiver) SendCompleted(ctx context.Context, _ *pdu.SendCompletedReq) (*pdu.SendCompletedRes, error) {
	defer trace.WithSpanFromStackUpdateCtx(&ctx)()

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
	zfs.ZFSDestroyFilesystemVersions(ctx, reqs)
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
