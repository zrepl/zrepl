package endpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/daemon/logging/trace"
	"github.com/zrepl/zrepl/util/envconst"
	"github.com/zrepl/zrepl/util/nodefault"
	"github.com/zrepl/zrepl/util/semaphore"
	"github.com/zrepl/zrepl/zfs"
)

type AbstractionType string

// Implementation note:
// There are a lot of exhaustive switches on AbstractionType in the code base.
// When adding a new abstraction type, make sure to search and update them!
const (
	AbstractionStepHold                           AbstractionType = "step-hold"
	AbstractionLastReceivedHold                   AbstractionType = "last-received-hold"
	AbstractionTentativeReplicationCursorBookmark AbstractionType = "tentative-replication-cursor-bookmark-v2"
	AbstractionReplicationCursorBookmarkV1        AbstractionType = "replication-cursor-bookmark-v1"
	AbstractionReplicationCursorBookmarkV2        AbstractionType = "replication-cursor-bookmark-v2"
)

var AbstractionTypesAll = map[AbstractionType]bool{
	AbstractionStepHold:                           true,
	AbstractionLastReceivedHold:                   true,
	AbstractionTentativeReplicationCursorBookmark: true,
	AbstractionReplicationCursorBookmarkV1:        true,
	AbstractionReplicationCursorBookmarkV2:        true,
}

// Implementation Note:
// Whenever you add a new accessor, adjust AbstractionJSON.MarshalJSON accordingly
type Abstraction interface {
	GetType() AbstractionType
	GetFS() string
	GetName() string
	GetFullPath() string
	GetJobID() *JobID // may return nil if the abstraction does not have a JobID
	GetCreateTXG() uint64
	GetFilesystemVersion() zfs.FilesystemVersion
	String() string
	// destroy the abstraction: either releases the hold or destroys the bookmark
	Destroy(context.Context) error
	json.Marshaler
}

func AbstractionEquals(a, b Abstraction) bool {
	if (a != nil) != (b != nil) {
		return false
	}
	if a == nil && b == nil {
		return true
	}
	var aJobId, bJobId JobID
	if aJid := a.GetJobID(); aJid != nil {
		aJobId = *aJid
	}
	if bJid := b.GetJobID(); bJid != nil {
		bJobId = *bJid
	}
	return a.GetType() == b.GetType() &&
		a.GetFS() == b.GetFS() &&
		a.GetName() == b.GetName() &&
		a.GetFullPath() == b.GetFullPath() &&
		aJobId == bJobId &&
		a.GetCreateTXG() == b.GetCreateTXG() &&
		zfs.FilesystemVersionEqualIdentity(a.GetFilesystemVersion(), b.GetFilesystemVersion()) &&
		a.String() == b.String()
}

func (t AbstractionType) Validate() error {
	switch t {
	case AbstractionStepHold:
		return nil
	case AbstractionLastReceivedHold:
		return nil
	case AbstractionTentativeReplicationCursorBookmark:
		return nil
	case AbstractionReplicationCursorBookmarkV1:
		return nil
	case AbstractionReplicationCursorBookmarkV2:
		return nil
	default:
		return errors.Errorf("unknown abstraction type %q", t)
	}
}

func (t AbstractionType) MustValidate() error {
	if err := t.Validate(); err != nil {
		panic(err)
	}
	return nil
}

type AbstractionJSON struct{ Abstraction }

var _ json.Marshaler = (*AbstractionJSON)(nil)

func (a AbstractionJSON) MarshalJSON() ([]byte, error) {
	type S struct {
		Type              AbstractionType
		FS                string
		Name              string
		FullPath          string
		JobID             *JobID // may return nil if the abstraction does not have a JobID
		CreateTXG         uint64
		FilesystemVersion zfs.FilesystemVersion
		String            string
	}
	v := S{
		Type:              a.Abstraction.GetType(),
		FS:                a.Abstraction.GetFS(),
		Name:              a.Abstraction.GetName(),
		FullPath:          a.Abstraction.GetFullPath(),
		JobID:             a.Abstraction.GetJobID(),
		CreateTXG:         a.Abstraction.GetCreateTXG(),
		FilesystemVersion: a.Abstraction.GetFilesystemVersion(),
		String:            a.Abstraction.String(),
	}
	return json.Marshal(v)
}

type AbstractionTypeSet map[AbstractionType]bool

func AbstractionTypeSetFromStrings(sts []string) (AbstractionTypeSet, error) {
	ats := make(map[AbstractionType]bool, len(sts))
	for i, t := range sts {
		at := AbstractionType(t)
		if err := at.Validate(); err != nil {
			return nil, errors.Wrapf(err, "invalid abstraction type #%d %q", i+1, t)
		}
		ats[at] = true
	}
	return ats, nil
}

func (s AbstractionTypeSet) ContainsAll(q AbstractionTypeSet) bool {
	for k := range q {
		if _, ok := s[k]; !ok {
			return false
		}
	}
	return true
}

func (s AbstractionTypeSet) ContainsAnyOf(q AbstractionTypeSet) bool {
	for k := range q {
		if _, ok := s[k]; ok {
			return true
		}
	}
	return false
}

func (s AbstractionTypeSet) String() string {
	sts := make([]string, 0, len(s))
	for i := range s {
		sts = append(sts, string(i))
	}
	sort.Strings(sts)
	return strings.Join(sts, ",")
}

func (s AbstractionTypeSet) Validate() error {
	for k := range s {
		if err := k.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type BookmarkExtractor func(fs *zfs.DatasetPath, v zfs.FilesystemVersion) Abstraction

// returns nil if the abstraction type is not bookmark-based
func (t AbstractionType) BookmarkExtractor() BookmarkExtractor {
	switch t {
	case AbstractionTentativeReplicationCursorBookmark:
		return TentativeReplicationCursorExtractor
	case AbstractionReplicationCursorBookmarkV1:
		return ReplicationCursorV1Extractor
	case AbstractionReplicationCursorBookmarkV2:
		return ReplicationCursorV2Extractor
	case AbstractionStepHold:
		return nil
	case AbstractionLastReceivedHold:
		return nil
	default:
		panic(fmt.Sprintf("unimpl: %q", t))
	}
}

type HoldExtractor = func(fs *zfs.DatasetPath, v zfs.FilesystemVersion, tag string) Abstraction

// returns nil if the abstraction type is not hold-based
func (t AbstractionType) HoldExtractor() HoldExtractor {
	switch t {
	case AbstractionTentativeReplicationCursorBookmark:
		return nil
	case AbstractionReplicationCursorBookmarkV1:
		return nil
	case AbstractionReplicationCursorBookmarkV2:
		return nil
	case AbstractionStepHold:
		return StepHoldExtractor
	case AbstractionLastReceivedHold:
		return LastReceivedHoldExtractor
	default:
		panic(fmt.Sprintf("unimpl: %q", t))
	}
}

func (t AbstractionType) BookmarkNamer() func(fs string, guid uint64, jobId JobID) (string, error) {
	switch t {
	case AbstractionTentativeReplicationCursorBookmark:
		return TentativeReplicationCursorBookmarkName
	case AbstractionReplicationCursorBookmarkV1:
		panic("shouldn't be creating new ones")
	case AbstractionReplicationCursorBookmarkV2:
		return ReplicationCursorBookmarkName
	case AbstractionStepHold:
		return nil
	case AbstractionLastReceivedHold:
		return nil
	default:
		panic(fmt.Sprintf("unimpl: %q", t))
	}
}

type ListZFSHoldsAndBookmarksQuery struct {
	FS ListZFSHoldsAndBookmarksQueryFilesystemFilter
	// What abstraction types should match (any contained in the set)
	What AbstractionTypeSet

	// The output for the query must satisfy _all_ (AND) requirements of all fields in this query struct.

	// if not nil: JobID of the hold or bookmark in question must be equal
	// else: JobID of the hold or bookmark can be any value
	JobID *JobID

	// zero-value means any CreateTXG is acceptable
	CreateTXG CreateTXGRange

	// Number of concurrently queried filesystems. Must be >= 1
	Concurrency int64
}

type CreateTXGRangeBound struct {
	CreateTXG uint64
	Inclusive *nodefault.Bool // must not be nil
}

// A non-empty range of CreateTXGs
//
// If both Since and Until are nil, any CreateTXG is acceptable
type CreateTXGRange struct {
	// if not nil: The hold's snapshot or the bookmark's createtxg must be greater than (or equal) Since
	// else: CreateTXG of the hold or bookmark can be any value accepted by Until
	Since *CreateTXGRangeBound
	// if not nil: The hold's snapshot or the bookmark's createtxg must be less than (or equal) Until
	// else: CreateTXG of the hold or bookmark can be any value accepted by Since
	Until *CreateTXGRangeBound
}

// FS == nil XOR Filter == nil
type ListZFSHoldsAndBookmarksQueryFilesystemFilter struct {
	FS     *string
	Filter zfs.DatasetFilter
}

func (q *ListZFSHoldsAndBookmarksQuery) Validate() error {
	if err := q.FS.Validate(); err != nil {
		return errors.Wrap(err, "FS")
	}
	if q.JobID != nil {
		q.JobID.MustValidate() // FIXME
	}
	if err := q.CreateTXG.Validate(); err != nil {
		return errors.Wrap(err, "CreateTXGRange")
	}
	if err := q.What.Validate(); err != nil {
		return err
	}
	if q.Concurrency < 1 {
		return errors.New("Concurrency must be >= 1")
	}
	return nil
}

var createTXGRangeBoundAllowCreateTXG0 = envconst.Bool("ZREPL_ENDPOINT_LIST_ABSTRACTIONS_QUERY_CREATETXG_RANGE_BOUND_ALLOW_0", false)

func (i *CreateTXGRangeBound) Validate() error {
	if err := i.Inclusive.ValidateNoDefault(); err != nil {
		return errors.Wrap(err, "Inclusive")
	}
	if i.CreateTXG == 0 && !createTXGRangeBoundAllowCreateTXG0 {
		return errors.New("CreateTXG must be non-zero")
	}
	return nil

}

func (f *ListZFSHoldsAndBookmarksQueryFilesystemFilter) Validate() error {
	if f == nil {
		return nil
	}
	fsSet := f.FS != nil
	filterSet := f.Filter != nil
	if fsSet && filterSet || !fsSet && !filterSet {
		return fmt.Errorf("must set FS or Filter field, but fsIsSet=%v and filterIsSet=%v", fsSet, filterSet)
	}
	if fsSet {
		if err := zfs.EntityNamecheck(*f.FS, zfs.EntityTypeFilesystem); err != nil {
			return errors.Wrap(err, "FS invalid")
		}
	}
	return nil
}

func (f *ListZFSHoldsAndBookmarksQueryFilesystemFilter) Filesystems(ctx context.Context) ([]string, error) {
	if err := f.Validate(); err != nil {
		panic(err)
	}
	if f.FS != nil {
		return []string{*f.FS}, nil
	}
	if f.Filter != nil {
		dps, err := zfs.ZFSListMapping(ctx, f.Filter)
		if err != nil {
			return nil, err
		}
		fss := make([]string, len(dps))
		for i, dp := range dps {
			fss[i] = dp.ToString()
		}
		return fss, nil
	}
	panic("unreachable")
}

func (r *CreateTXGRange) Validate() error {
	if r.Since != nil {
		if err := r.Since.Validate(); err != nil {
			return errors.Wrap(err, "Since")
		}
	}
	if r.Until != nil {
		if err := r.Until.Validate(); err != nil {
			return errors.Wrap(err, "Until")
		}
	}
	if _, err := r.effectiveBounds(); err != nil {
		return errors.Wrapf(err, "specified range %s is semantically invalid", r)
	}
	return nil
}

// inclusive-inclusive bounds
type effectiveBounds struct {
	sinceInclusive uint64
	sinceUnbounded bool
	untilInclusive uint64
	untilUnbounded bool
}

// callers must have validated r.Since and r.Until before calling this method
func (r *CreateTXGRange) effectiveBounds() (bounds effectiveBounds, err error) {

	bounds.sinceUnbounded = r.Since == nil
	bounds.untilUnbounded = r.Until == nil

	if r.Since == nil && r.Until == nil {
		return bounds, nil
	}

	if r.Since != nil {
		bounds.sinceInclusive = r.Since.CreateTXG
		if !r.Since.Inclusive.B {
			if r.Since.CreateTXG == math.MaxUint64 {
				return bounds, errors.Errorf("Since-exclusive (%v) must be less than math.MaxUint64 (%v)",
					r.Since.CreateTXG, uint64(math.MaxUint64))
			}
			bounds.sinceInclusive++
		}
	}

	if r.Until != nil {
		bounds.untilInclusive = r.Until.CreateTXG
		if !r.Until.Inclusive.B {
			if r.Until.CreateTXG == 0 {
				return bounds, errors.Errorf("Until-exclusive (%v) must be greater than 0", r.Until.CreateTXG)
			}
			bounds.untilInclusive--
		}
	}

	if !bounds.sinceUnbounded && !bounds.untilUnbounded {
		if bounds.sinceInclusive >= bounds.untilInclusive {
			return bounds, errors.Errorf("effective range bounds are [%v,%v] which is empty or invalid", bounds.sinceInclusive, bounds.untilInclusive)
		}
		// fallthrough
	}

	return bounds, nil
}

func (r *CreateTXGRange) String() string {
	var buf strings.Builder
	if r.Since == nil {
		fmt.Fprintf(&buf, "~")
	} else {
		if err := r.Since.Inclusive.ValidateNoDefault(); err != nil {
			fmt.Fprintf(&buf, "?")
		} else if r.Since.Inclusive.B {
			fmt.Fprintf(&buf, "[")
		} else {
			fmt.Fprintf(&buf, "(")
		}
		fmt.Fprintf(&buf, "%d", r.Since.CreateTXG)
	}

	fmt.Fprintf(&buf, ",")

	if r.Until == nil {
		fmt.Fprintf(&buf, "~")
	} else {
		fmt.Fprintf(&buf, "%d", r.Until.CreateTXG)
		if err := r.Until.Inclusive.ValidateNoDefault(); err != nil {
			fmt.Fprintf(&buf, "?")
		} else if r.Until.Inclusive.B {
			fmt.Fprintf(&buf, "]")
		} else {
			fmt.Fprintf(&buf, ")")
		}
	}

	return buf.String()
}

// panics if not .Validate()
func (r *CreateTXGRange) IsUnbounded() bool {
	if err := r.Validate(); err != nil {
		panic(err)
	}
	bounds, err := r.effectiveBounds()
	if err != nil {
		panic(err)
	}
	return bounds.sinceUnbounded && bounds.untilUnbounded
}

// panics if not .Validate()
func (r *CreateTXGRange) Contains(qCreateTxg uint64) bool {
	if err := r.Validate(); err != nil {
		panic(err)
	}

	bounds, err := r.effectiveBounds()
	if err != nil {
		panic(err)
	}

	sinceMatches := bounds.sinceUnbounded || bounds.sinceInclusive <= qCreateTxg
	untilMatches := bounds.untilUnbounded || qCreateTxg <= bounds.untilInclusive

	return sinceMatches && untilMatches
}

type ListAbstractionsError struct {
	FS   string
	Snap string
	What string
	Err  error
}

func (e ListAbstractionsError) Error() string {
	if e.FS == "" {
		return fmt.Sprintf("list endpoint abstractions: %s: %s", e.What, e.Err)
	} else {
		v := e.FS
		if e.Snap != "" {
			v = fmt.Sprintf("%s@%s", e.FS, e.Snap)
		}
		return fmt.Sprintf("list endpoint abstractions on %q: %s: %s", v, e.What, e.Err)
	}
}

type putListAbstractionErr func(err error, fs string, what string)
type putListAbstraction func(a Abstraction)

type ListAbstractionsErrors []ListAbstractionsError

func (e ListAbstractionsErrors) Error() string {
	if len(e) == 0 {
		panic(e)
	}
	if len(e) == 1 {
		return fmt.Sprintf("list endpoint abstractions: %s", e[0])
	}
	msgs := make([]string, len(e))
	for i := range e {
		msgs[i] = e[i].Error()
	}
	return fmt.Sprintf("list endpoint abstractions: multiple errors:\n%s", strings.Join(msgs, "\n"))
}

func ListAbstractions(ctx context.Context, query ListZFSHoldsAndBookmarksQuery) (out []Abstraction, outErrs []ListAbstractionsError, err error) {
	outChan, outErrsChan, drainDone, err := ListAbstractionsStreamed(ctx, query)
	if err != nil {
		return nil, nil, err
	}
	defer drainDone()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for a := range outChan {
			out = append(out, a)
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for err := range outErrsChan {
			outErrs = append(outErrs, err)
		}
	}()
	wg.Wait()
	return out, outErrs, nil
}

// if err != nil, the returned channels are both nil
// if err == nil, both channels must be fully drained by the caller to avoid leaking goroutines.
// After draining is done, the caller must call the returned drainDone func.
func ListAbstractionsStreamed(ctx context.Context, query ListZFSHoldsAndBookmarksQuery) (_ <-chan Abstraction, _ <-chan ListAbstractionsError, drainDone func(), _ error) {

	// impl note: structure the query processing in such a way that
	// a minimum amount of zfs shell-outs needs to be done

	if err := query.Validate(); err != nil {
		return nil, nil, nil, errors.Wrap(err, "validate query")
	}

	fss, err := query.FS.Filesystems(ctx)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "list filesystems")
	}

	outErrs := make(chan ListAbstractionsError)
	out := make(chan Abstraction)

	errCb := func(err error, fs string, what string) {
		outErrs <- ListAbstractionsError{Err: err, FS: fs, What: what}
	}
	emitAbstraction := func(a Abstraction) {
		jobIdMatches := query.JobID == nil || a.GetJobID() == nil || *a.GetJobID() == *query.JobID

		createTXGMatches := query.CreateTXG.Contains(a.GetCreateTXG())

		if jobIdMatches && createTXGMatches {
			out <- a
		}
	}

	sem := semaphore.New(int64(query.Concurrency))
	ctx, endTask := trace.WithTask(ctx, "list-abstractions-streamed-producer")
	go func() {
		defer close(out)
		defer close(outErrs)

		_, add, wait := trace.WithTaskGroup(ctx, "list-abstractions-impl-fs")
		defer wait()
		for _, fs := range fss {
			fs := fs // capture by copy
			add(func(ctx context.Context) {
				g, err := sem.Acquire(ctx)
				if err != nil {
					errCb(err, fs, err.Error())
					return
				}
				func() {
					defer g.Release()
					listAbstractionsImplFS(ctx, fs, &query, emitAbstraction, errCb)
				}()
			})
		}
	}()

	drainDone = func() {
		endTask()
	}

	return out, outErrs, drainDone, nil
}

func listAbstractionsImplFS(ctx context.Context, fs string, query *ListZFSHoldsAndBookmarksQuery, emitCandidate putListAbstraction, errCb putListAbstractionErr) {
	fsp, err := zfs.NewDatasetPath(fs)
	if err != nil {
		panic(err)
	}

	if len(query.What) == 0 {
		return
	}

	whatTypes := zfs.VersionTypeSet{}
	for what := range query.What {
		if e := what.BookmarkExtractor(); e != nil {
			whatTypes[zfs.Bookmark] = true
		}
		if e := what.HoldExtractor(); e != nil {
			whatTypes[zfs.Snapshot] = true
		}
	}
	fsvs, err := zfs.ZFSListFilesystemVersions(ctx, fsp, zfs.ListFilesystemVersionsOptions{
		Types: whatTypes,
	})
	if err != nil {
		errCb(err, fs, "list filesystem versions")
		return
	}

	for at := range query.What {
		bmE := at.BookmarkExtractor()
		holdE := at.HoldExtractor()
		if bmE == nil && holdE == nil || bmE != nil && holdE != nil {
			panic("implementation error: extractors misconfigured for " + at)
		}
		for _, v := range fsvs {
			if v.Type == zfs.Bookmark && bmE != nil {
				if a := bmE(fsp, v); a != nil {
					emitCandidate(a)
				}
			}
			if v.Type == zfs.Snapshot && holdE != nil && query.CreateTXG.Contains(v.GetCreateTXG()) && (!v.UserRefs.Valid || v.UserRefs.Value > 0) { // FIXME review v.UserRefsValid
				holds, err := zfs.ZFSHolds(ctx, fsp.ToString(), v.Name)
				if err != nil {
					errCb(err, v.ToAbsPath(fsp), "get hold on snap")
					continue
				}
				for _, tag := range holds {
					if a := holdE(fsp, v, tag); a != nil {
						emitCandidate(a)
					}
				}
			}
		}
	}
}

type BatchDestroyResult struct {
	Abstraction
	DestroyErr error
}

var _ json.Marshaler = (*BatchDestroyResult)(nil)

func (r BatchDestroyResult) MarshalJSON() ([]byte, error) {
	err := ""
	if r.DestroyErr != nil {
		err = r.DestroyErr.Error()
	}
	s := struct {
		Abstraction AbstractionJSON
		DestroyErr  string
	}{
		AbstractionJSON{r.Abstraction},
		err,
	}
	return json.Marshal(s)
}

func BatchDestroy(ctx context.Context, abs []Abstraction) <-chan BatchDestroyResult {
	// hold-based batching: per snapshot
	// bookmark-based batching: none possible via CLI
	// => not worth the trouble for now, will be worth it once we start using channel programs
	// => TODO: actual batching using channel programs
	res := make(chan BatchDestroyResult, len(abs))
	go func() {
		for _, a := range abs {
			res <- BatchDestroyResult{
				a,
				a.Destroy(ctx),
			}
		}
		close(res)
	}()
	return res
}

type StalenessInfo struct {
	ConstructedWithQuery ListZFSHoldsAndBookmarksQuery
	Live                 []Abstraction
	Stale                []Abstraction
}

type fsAndJobId struct {
	fs    string
	jobId JobID
}

type ListStaleQueryError struct {
	error
}

// returns *ListStaleQueryError if the given query cannot be used for determining staleness info
func ListStale(ctx context.Context, q ListZFSHoldsAndBookmarksQuery) (*StalenessInfo, error) {
	if !q.CreateTXG.IsUnbounded() {
		// we must determine the most recent step per FS, can't allow that
		return nil, &ListStaleQueryError{errors.New("ListStale cannot have Until != nil set on query")}
	}

	// if asking for step holds must also ask for replication cursor bookmarks (for firstNotStale)
	ifAnyThenAll := AbstractionTypeSet{
		AbstractionStepHold:                    true,
		AbstractionReplicationCursorBookmarkV2: true,
	}
	if q.What.ContainsAnyOf(ifAnyThenAll) && !q.What.ContainsAll(ifAnyThenAll) {
		return nil, &ListStaleQueryError{errors.Errorf("ListStale requires query to ask for all of %s", ifAnyThenAll.String())}
	}

	// ----------------- done validating query for listStaleFiltering -----------------------

	qAbs, absErr, err := ListAbstractions(ctx, q)
	if err != nil {
		return nil, err
	}
	if len(absErr) > 0 {
		// can't go on here because we can't determine the most recent step
		return nil, ListAbstractionsErrors(absErr)
	}

	si := listStaleFiltering(qAbs, q.CreateTXG.Since)
	si.ConstructedWithQuery = q
	return si, nil
}

type fsAjobAtype struct {
	fsAndJobId
	Type AbstractionType
}

// For step holds and bookmarks, only those older than the most recent replication cursor
// of their (filesystem,job) are considered because younger ones cannot be stale by definition
// (if we destroy them, we might actually lose the hold on the `To` for an ongoing incremental replication)
//
// For replication cursors and last-received-holds, only the most recent one is kept.
//
// the returned StalenessInfo.ConstructedWithQuery is not set
func listStaleFiltering(abs []Abstraction, sinceBound *CreateTXGRangeBound) *StalenessInfo {

	var noJobId []Abstraction
	by := make(map[fsAjobAtype][]Abstraction)
	for _, a := range abs {
		if a.GetJobID() == nil {
			noJobId = append(noJobId, a)
			continue
		}
		faj := fsAjobAtype{fsAndJobId{a.GetFS(), *a.GetJobID()}, a.GetType()}
		l := by[faj]
		l = append(l, a)
		by[faj] = l
	}

	type stepFirstNotStaleCandidate struct {
		cursor *Abstraction
		step   *Abstraction
	}
	stepFirstNotStaleCandidates := make(map[fsAndJobId]stepFirstNotStaleCandidate) // empty map => will always return nil
	for _, a := range abs {
		if a.GetJobID() == nil {
			continue // already put those into always-live list noJobId in above loop
		}
		key := fsAndJobId{a.GetFS(), *a.GetJobID()}
		c := stepFirstNotStaleCandidates[key]

		switch a.GetType() {
		// stepFirstNotStaleCandidate.cursor
		case AbstractionReplicationCursorBookmarkV2:
			if c.cursor == nil || (*c.cursor).GetCreateTXG() < a.GetCreateTXG() {
				a := a
				c.cursor = &a
			}

		// stepFirstNotStaleCandidate.step
		case AbstractionStepHold:
			if c.step == nil || (*c.step).GetCreateTXG() < a.GetCreateTXG() {
				a := a
				c.step = &a
			}

		// not interested in the others
		default:
			continue // not relevant

		}

		stepFirstNotStaleCandidates[key] = c
	}

	ret := &StalenessInfo{
		Live:  noJobId,
		Stale: []Abstraction{},
	}

	for k := range by {
		l := by[k]

		if k.Type == AbstractionStepHold {
			// all step holds older than the most recent cursor are stale, others are always live

			// if we don't have a replication cursor yet, use untilBound = nil
			// to consider all steps stale (...at first)
			var untilBound *CreateTXGRangeBound
			{
				sfnsc := stepFirstNotStaleCandidates[k.fsAndJobId]

				// If there's a replication cursor, use it as a cutoff between live and stale
				// for both cursors and holds.
				// If there's no cursor, we are in initial replication and only need to keep
				// the most recent step hold live, which hold the .To of the initial send step.
				if sfnsc.cursor != nil {
					untilBound = &CreateTXGRangeBound{
						CreateTXG: (*sfnsc.cursor).GetCreateTXG(),
						// if we have a cursor, can throw away step hold on both From and To
						Inclusive: &nodefault.Bool{B: true},
					}
				} else if sfnsc.step != nil {
					untilBound = &CreateTXGRangeBound{
						CreateTXG: (*sfnsc.step).GetCreateTXG(),
						// if we don't have a cursor, the step most recent step hold is our
						// initial replication cursor and it's possibly still live (interrupted initial replication)
						Inclusive: &nodefault.Bool{B: false},
					}
				} else {
					untilBound = nil // consider everything stale
				}
			}
			staleRange := CreateTXGRange{
				Since: sinceBound,
				Until: untilBound,
			}

			// partition by staleRange
			for _, a := range l {
				if staleRange.Contains(a.GetCreateTXG()) {
					ret.Stale = append(ret.Stale, a)
				} else {
					ret.Live = append(ret.Live, a)
				}
			}

		} else if k.Type == AbstractionReplicationCursorBookmarkV2 || k.Type == AbstractionLastReceivedHold {
			// all cursors but the most recent cursor are stale by definition (we always _move_ them)
			// NOTE: must not use firstNotStale in this branch, not computed for these types

			// sort descending (highest createtxg first), then cut off
			sort.Slice(l, func(i, j int) bool {
				return l[i].GetCreateTXG() > l[j].GetCreateTXG()
			})
			if len(l) > 0 {
				ret.Live = append(ret.Live, l[0])
				ret.Stale = append(ret.Stale, l[1:]...)
			}
		} else {
			ret.Live = append(ret.Live, l...)
		}
	}

	return ret

}
