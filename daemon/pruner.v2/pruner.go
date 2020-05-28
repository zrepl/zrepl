package pruner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/daemon/logging"
	"github.com/zrepl/zrepl/daemon/logging/trace"
	"github.com/zrepl/zrepl/endpoint"
	"github.com/zrepl/zrepl/pruning"
	"github.com/zrepl/zrepl/zfs"
)

type Pruner struct {
	fsfilter  endpoint.FSFilter
	jid       endpoint.JobID
	side      Side
	keepRules []pruning.KeepRule

	// all channels consumed by the run loop
	reportReqs chan reportRequest
	stopReqs   chan stopRequest
	done       chan struct{}
	fsListRes  chan fsListRes

	state State

	listFilesystemsError error       // only in state StateListFilesystemsError
	fsPruners            []*FSPruner // only in state StateFanOutFilesystems
}

//go:generate enumer -type=State -json
type State int

const (
	StateInitialized State = iota
	StateListFilesystems
	StateListFilesystemsError
	StateFanOutFilesystems
	StateDone
)

type Report struct {
	State                State
	ListFilesystemsError error       // only valid in StateListFilesystemsError
	Filesystems          []*FSReport // valid from StateFanOutFilesystems
}

type reportRequest struct {
	ctx   context.Context
	reply chan *Report
}

type runRequest struct {
	complete chan struct{}
}

type stopRequest struct {
	complete chan struct{}
}

type fsListRes struct {
	filesystems []*zfs.DatasetPath
	err         error
}

type Side interface {
	// may return both nil, indicating there is no replication position
	GetReplicationPosition(ctx context.Context, fs string) (*zfs.FilesystemVersion, error)
	isSide() Side
}

func NewPruner(fsfilter endpoint.FSFilter, jid endpoint.JobID, side Side, keepRules []pruning.KeepRule) *Pruner {
	return &Pruner{
		fsfilter,
		jid,
		side,
		keepRules,
		make(chan reportRequest),
		make(chan stopRequest),
		make(chan struct{}),
		make(chan fsListRes),
		StateInitialized,
		nil,
		nil,
	}
}

func (p *Pruner) Run(ctx context.Context) *Report {

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if p.state != StateInitialized {
		panic("Run can onl[y be called once")
	}

	go func() {
		fss, err := zfs.ZFSListMapping(ctx, p.fsfilter)
		p.fsListRes <- fsListRes{fss, err}
	}()

	for {
		select {
		case res := <-p.fsListRes:
			if res.err != nil {
				p.state = StateListFilesystemsError
				p.listFilesystemsError = res.err
				close(p.done)
				continue
			}

			p.state = StateFanOutFilesystems

			p.fsPruners = make([]*FSPruner, len(res.filesystems))
			_, add, end := trace.WithTaskGroup(ctx, "pruner-fan-out-fs")
			for i, fs := range res.filesystems {
				p.fsPruners[i] = NewFSPruner(p.jid, p.side, p.keepRules, fs)
				add(func(ctx context.Context) {
					p.fsPruners[i].Run(ctx)
				})
			}
			go func() {
				end()
				close(p.done)
			}()

		case req := <-p.stopReqs:
			cancel()
			go func() {
				<-p.done
				close(req.complete)
			}()
		case req := <-p.reportReqs:
			req.reply <- p.report(req.ctx)
		case <-p.done:
			p.state = StateDone
			return p.report(ctx)
		}
	}
}

func (p *Pruner) Report(ctx context.Context) *Report {
	req := reportRequest{
		ctx:   ctx,
		reply: make(chan *Report, 1),
	}
	select {
	case p.reportReqs <- req:
		return <-req.reply
	case <-ctx.Done():
		return nil
	case <-p.done:
		return nil
	}
}

func (p *Pruner) report(ctx context.Context) *Report {
	fsreports := make([]*FSReport, len(p.fsPruners))
	for i := range fsreports {
		fsreports[i] = p.fsPruners[i].report()
	}
	return &Report{
		State:                p.state,
		ListFilesystemsError: p.listFilesystemsError,
		Filesystems:          fsreports,
	}
}

// implements pruning.Snapshot
type snapshot struct {
	replicated bool
	stepHolds  []pruning.StepHold
	zfs.FilesystemVersion

	state     SnapState
	destroyOp *zfs.DestroySnapOp
}

//go:generate enumer -type=SnapState -json
type SnapState int

const (
	SnapStateInitialized SnapState = iota
	SnapStateKeeping
	SnapStateDeletePending
	SnapStateDeleteAttempted
)

// implements pruning.StepHold
type stepHold struct {
	endpoint.Abstraction
}

func (s snapshot) Replicated() bool              { return s.replicated }
func (s snapshot) StepHolds() []pruning.StepHold { return s.stepHolds }

func (s stepHold) GetJobID() endpoint.JobID { return *s.Abstraction.GetJobID() }

type FSPruner struct {
	jid       endpoint.JobID
	side      Side
	keepRules []pruning.KeepRule
	fsp       *zfs.DatasetPath

	state FSState

	// all channels consumed by the run loop
	planned    chan fsPlanRes
	executed   chan fsExecuteRes
	done       chan struct{}
	reportReqs chan fsReportReq

	keepList    []*snapshot // valid in FSStateExecuting and forward
	destroyList []*snapshot // valid in FSStateExecuting and forward, field .destroyOp is invalid until FSStateExecuting is left

}

type fsPlanRes struct {
	keepList    []*snapshot
	destroyList []*snapshot
	err         error
}

type fsExecuteRes struct {
	completedDestroyOps []*zfs.DestroySnapOp // same len() as FSPruner.destroyList
}

type fsReportReq struct {
	res chan *FSReport
}

type FSReport struct {
	State    FSState
	KeepList []*SnapReport
	Destroy  []*SnapReport
}

type SnapReport struct {
	State         SnapState
	Name          string
	Replicated    bool
	StepHoldCount int
	DestroyError  error
}

//go:generate enumer -type=FSState -json
type FSState int

const (
	FSStateInitialized FSState = iota
	FSStatePlanning
	FSStatePlanErr
	FSStateExecuting
	FSStateExecuteErr
	FSStateExecuteSuccess
)

func (s FSState) IsTerminal() bool {
	return s == FSStatePlanErr || s == FSStateExecuteErr || s == FSStateExecuteSuccess
}

func NewFSPruner(jid endpoint.JobID, side Side, keepRules []pruning.KeepRule, fsp *zfs.DatasetPath) *FSPruner {
	return &FSPruner{
		jid, side, keepRules, fsp,
		FSStateInitialized,
		make(chan fsPlanRes),
		make(chan fsExecuteRes),
		make(chan struct{}),
		make(chan fsReportReq),
		nil, nil,
	}
}

func (p *FSPruner) Run(ctx context.Context) *FSReport {

	defer func() {
	}()

	p.state = FSStatePlanning

	go func() { p.planned <- p.plan(ctx) }()

out:
	for !p.state.IsTerminal() {
		select {
		case res := <-p.planned:

			if res.err != nil {
				p.state = FSStatePlanErr
				continue
			}
			p.state = FSStateExecuting
			p.keepList = res.keepList
			p.destroyList = res.destroyList

			go func() { p.executed <- p.execute(ctx, p.destroyList) }()

		case res := <-p.executed:

			if len(res.completedDestroyOps) != len(p.destroyList) {
				panic("impl error: completedDestroyOps is a vector corresponding to entries in p.destroyList")
			}

			var erronous []*zfs.DestroySnapOp
			for i, op := range res.completedDestroyOps {
				if *op.ErrOut != nil {
					erronous = append(erronous, op)
				}
				p.destroyList[i].destroyOp = op
				p.destroyList[i].state = SnapStateDeleteAttempted
			}
			if len(erronous) > 0 {
				p.state = FSStateExecuteErr
			} else {
				p.state = FSStateExecuteSuccess
			}

			close(p.done)

		case <-p.reportReqs:
			panic("unimp")
		case <-p.done:
			break out
		}
	}

	// TODO render last FS report
	return nil
}

func (p *FSPruner) plan(ctx context.Context) fsPlanRes {
	fs := p.fsp.ToString()
	vs, err := zfs.ZFSListFilesystemVersions(ctx, p.fsp, zfs.ListFilesystemVersionsOptions{})
	if err != nil {
		return fsPlanRes{err: errors.Wrap(err, "list filesystem versions")}
	}

	allJobsStepHolds, absErrs, err := endpoint.ListAbstractions(ctx, endpoint.ListZFSHoldsAndBookmarksQuery{
		FS: endpoint.ListZFSHoldsAndBookmarksQueryFilesystemFilter{
			FS: &fs,
		},
		What: endpoint.AbstractionTypeSet{
			endpoint.AbstractionStepHold: true,
		},
		Concurrency: 1,
	})
	if err != nil {
		return fsPlanRes{err: errors.Wrap(err, "list abstractions")}
	}
	if len(absErrs) > 0 {
		logging.GetLogger(ctx, logging.SubsysPruning).WithError(endpoint.ListAbstractionsErrors(absErrs)).
			Error("error listing some step holds, prune attempt might fail with 'dataset is busy' errors")
	}

	repPos, err := p.side.GetReplicationPosition(ctx, p.fsp.ToString())
	if err != nil {
		return fsPlanRes{err: errors.Wrap(err, "get replication position")}
	}

	vsAsSnaps := make([]pruning.Snapshot, len(vs))
	for i := range vs {
		var repPosCreateTxgOrZero uint64
		if repPos != nil {
			repPosCreateTxgOrZero = repPos.GetCreateTXG()
		}
		s := &snapshot{
			state:             SnapStateInitialized,
			FilesystemVersion: vs[i],
			replicated:        vs[i].GetCreateTXG() <= repPosCreateTxgOrZero,
		}
		for _, h := range allJobsStepHolds {
			if zfs.FilesystemVersionEqualIdentity(vs[i], h.GetFilesystemVersion()) {
				s.stepHolds = append(s.stepHolds, stepHold{h})
			}
		}
		vsAsSnaps[i] = s
	}

	downcastToSnapshots := func(l []pruning.Snapshot) (r []*snapshot) {
		r = make([]*snapshot, len(l))
		for i, e := range l {
			r[i] = e.(*snapshot)
		}
		return r
	}
	pruningResult := pruning.PruneSnapshots(vsAsSnaps, p.keepRules)
	remove, keep := downcastToSnapshots(pruningResult.Remove), downcastToSnapshots(pruningResult.Keep)
	if len(remove)+len(keep) != len(vsAsSnaps) {
		for _, s := range vsAsSnaps {
			r, _ := json.MarshalIndent(s.(*snapshot).report(), "", "  ")
			fmt.Fprintf(os.Stderr, "%s\n", string(r))
		}
		panic("indecisive")
	}

	for _, s := range remove {
		s.state = SnapStateDeletePending
	}
	for _, s := range keep {
		s.state = SnapStateKeeping
	}

	return fsPlanRes{keepList: keep, destroyList: remove, err: nil}
}

func (p *FSPruner) execute(ctx context.Context, destroyList []*snapshot) fsExecuteRes {
	ops := make([]*zfs.DestroySnapOp, len(destroyList))
	for i, fsv := range p.destroyList {
		ops[i] = &zfs.DestroySnapOp{
			Filesystem: p.fsp.ToString(),
			Name:       fsv.GetName(),
			ErrOut:     new(error),
		}
	}
	zfs.ZFSDestroyFilesystemVersions(ctx, ops)

	return fsExecuteRes{completedDestroyOps: ops}
}

func (p *FSPruner) report() *FSReport {
	return &FSReport{
		State:    p.state,
		KeepList: p.reportRenderSnapReports(p.keepList),
		Destroy:  p.reportRenderSnapReports(p.destroyList),
	}
}

func (p *FSPruner) reportRenderSnapReports(l []*snapshot) (r []*SnapReport) {
	r = make([]*SnapReport, len(l))
	for i := range l {
		r[i] = l[i].report()
	}
	return r
}

func (s *snapshot) report() *SnapReport {
	var snapErr error
	if s.state == SnapStateDeleteAttempted {
		if *s.destroyOp.ErrOut != nil {
			snapErr = (*s.destroyOp.ErrOut)
		}
	}
	return &SnapReport{
		State:         s.state,
		Name:          s.Name,
		Replicated:    s.Replicated(),
		StepHoldCount: len(s.stepHolds),
		DestroyError:  snapErr,
	}
}
