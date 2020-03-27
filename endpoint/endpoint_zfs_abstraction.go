package endpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/util/envconst"
	"github.com/zrepl/zrepl/zfs"
)

type AbstractionType string

// Implementation note:
// There are a lot of exhaustive switches on AbstractionType in the code base.
// When adding a new abstraction type, make sure to search and update them!
const (
	AbstractionStepBookmark                AbstractionType = "step-bookmark"
	AbstractionStepHold                    AbstractionType = "step-hold"
	AbstractionLastReceivedHold            AbstractionType = "last-received-hold"
	AbstractionReplicationCursorBookmarkV1 AbstractionType = "replication-cursor-bookmark-v1"
	AbstractionReplicationCursorBookmarkV2 AbstractionType = "replication-cursor-bookmark-v2"
)

var AbstractionTypesAll = map[AbstractionType]bool{
	AbstractionStepBookmark:                true,
	AbstractionStepHold:                    true,
	AbstractionLastReceivedHold:            true,
	AbstractionReplicationCursorBookmarkV1: true,
	AbstractionReplicationCursorBookmarkV2: true,
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

func (t AbstractionType) Validate() error {
	switch t {
	case AbstractionStepBookmark:
		return nil
	case AbstractionStepHold:
		return nil
	case AbstractionLastReceivedHold:
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

// Number of instances of this abstraction type that are live (not stale)
// per (FS,JobID). -1 for infinity.
func (t AbstractionType) NumLivePerFsAndJob() int {
	switch t {
	case AbstractionStepBookmark:
		return 2
	case AbstractionStepHold:
		return 2
	case AbstractionLastReceivedHold:
		return 1
	case AbstractionReplicationCursorBookmarkV1:
		return -1
	case AbstractionReplicationCursorBookmarkV2:
		return 1
	default:
		panic(t)
	}
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

func (s AbstractionTypeSet) String() string {
	sts := make([]string, 0, len(s))
	for i := range s {
		sts = append(sts, string(i))
	}
	sts = sort.StringSlice(sts)
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
	case AbstractionStepBookmark:
		return StepBookmarkExtractor
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
	case AbstractionStepBookmark:
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

type ListZFSHoldsAndBookmarksQuery struct {
	FS ListZFSHoldsAndBookmarksQueryFilesystemFilter
	// What abstraction types should match (any contained in the set)
	What AbstractionTypeSet

	// The output for the query must satisfy _all_ (AND) requirements of all fields in this query struct.

	// if not nil: JobID of the hold or bookmark in question must be equal
	// else: JobID of the hold or bookmark can be any value
	JobID *JobID
	// if not nil: The hold's snapshot or the bookmark's createtxg must be less than (or equal) Until
	// else: CreateTXG of the hold or bookmark can be any value
	Until *InclusiveExclusiveCreateTXG

	// TODO
	// Concurrent: uint > 0
}

type InclusiveExclusiveCreateTXG struct {
	CreateTXG uint64
	Inclusive *zfs.NilBool // must not be nil
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
	if q.Until != nil {
		if err := q.Until.Validate(); err != nil {
			return errors.Wrap(err, "Until")
		}
	}
	if err := q.What.Validate(); err != nil {
		return err
	}
	return nil
}

var zreplEndpointListAbstractionsQueryCreatetxg0Allowed = envconst.Bool("ZREPL_ENDPOINT_LIST_ABSTRACTIONS_QUERY_CREATETXG_0_ALLOWED", false)

func (i *InclusiveExclusiveCreateTXG) Validate() error {
	if err := i.Inclusive.Validate(); err != nil {
		return errors.Wrap(err, "Inclusive")
	}
	if i.CreateTXG == 0 && !zreplEndpointListAbstractionsQueryCreatetxg0Allowed {
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
		msgs[i] = e.Error()
	}
	return fmt.Sprintf("list endpoint abstractions: multiple errors:\n%s", strings.Join(msgs, "\n"))
}

func ListAbstractions(ctx context.Context, query ListZFSHoldsAndBookmarksQuery) (out []Abstraction, outErrs []ListAbstractionsError, err error) {
	// impl note: structure the query processing in such a way that
	// a minimum amount of zfs shell-outs needs to be done

	if err := query.Validate(); err != nil {
		return nil, nil, errors.Wrap(err, "validate query")
	}

	fss, err := query.FS.Filesystems(ctx)
	if err != nil {
		return nil, nil, errors.Wrap(err, "list filesystems")
	}

	errCb := func(err error, fs string, what string) {
		outErrs = append(outErrs, ListAbstractionsError{Err: err, FS: fs, What: what})
	}
	emitAbstraction := func(a Abstraction) {
		jobIdMatches := query.JobID == nil || a.GetJobID() == nil || *a.GetJobID() == *query.JobID

		untilMatches := query.Until == nil
		if query.Until != nil {
			if query.Until.Inclusive.B {
				untilMatches = a.GetCreateTXG() <= query.Until.CreateTXG
			} else {
				untilMatches = a.GetCreateTXG() < query.Until.CreateTXG
			}
		}

		if jobIdMatches && untilMatches {
			out = append(out, a)
		}
	}
	for _, fs := range fss {
		listAbstractionsImplFS(ctx, fs, &query, emitAbstraction, errCb)
	}

	return out, outErrs, nil

}

func listAbstractionsImplFS(ctx context.Context, fs string, query *ListZFSHoldsAndBookmarksQuery, emitCandidate putListAbstraction, errCb putListAbstractionErr) {
	fsp, err := zfs.NewDatasetPath(fs)
	if err != nil {
		panic(err)
	}

	if len(query.What) == 0 {
		return
	}

	// we need filesystem versions for any abstraction type
	fsvs, err := zfs.ZFSListFilesystemVersions(fsp, nil)
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
			var a Abstraction
			if v.Type == zfs.Bookmark && bmE != nil {
				a = bmE(fsp, v)
			}
			if v.Type == zfs.Snapshot && holdE != nil {
				holds, err := zfs.ZFSHolds(ctx, fsp.ToString(), v.Name)
				if err != nil {
					errCb(err, v.ToAbsPath(fsp), "get hold on snap")
					continue
				}
				for _, tag := range holds {
					a = holdE(fsp, v, tag)
				}
			}
			if a != nil {
				emitCandidate(a)
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
	All                  []Abstraction
	Live                 []Abstraction
	Stale                []Abstraction
}

func ListStale(ctx context.Context, q ListZFSHoldsAndBookmarksQuery) (*StalenessInfo, error) {
	if q.Until != nil {
		// we must determine the most recent step per FS, can't allow that
		return nil, errors.New("ListStale cannot have Until != nil set on query")
	}

	abs, absErr, err := ListAbstractions(ctx, q)
	if err != nil {
		return nil, err
	}
	if len(absErr) > 0 {
		// can't go on here because we can't determine the most recent step
		return nil, ListAbstractionsErrors(absErr)
	}
	si := listStaleFiltering(abs)
	si.ConstructedWithQuery = q
	return si, nil
}

// The last AbstractionType.NumLive() step holds per (FS,Job,AbstractionType) are live
// others are stale.
//
// the returned StalenessInfo.ConstructedWithQuery is not set
func listStaleFiltering(abs []Abstraction) *StalenessInfo {

	type fsAjobAtype struct {
		FS   string
		Job  JobID
		Type AbstractionType
	}
	var noJobId []Abstraction
	by := make(map[fsAjobAtype][]Abstraction)
	for _, a := range abs {
		if a.GetJobID() == nil {
			noJobId = append(noJobId, a)
			continue
		}
		faj := fsAjobAtype{a.GetFS(), *a.GetJobID(), a.GetType()}
		l := by[faj]
		l = append(l, a)
		by[faj] = l
	}

	ret := &StalenessInfo{
		All:   abs,
		Live:  noJobId,
		Stale: []Abstraction{},
	}

	// sort descending (highest createtxg first), then cut off
	for k := range by {
		l := by[k]
		sort.Slice(l, func(i, j int) bool {
			return l[i].GetCreateTXG() > l[j].GetCreateTXG()
		})

		cutoff := k.Type.NumLivePerFsAndJob()
		if cutoff == -1 || len(l) <= cutoff {
			ret.Live = append(ret.Live, l...)
		} else {
			ret.Live = append(ret.Live, l[0:cutoff]...)
			ret.Stale = append(ret.Stale, l[cutoff:]...)
		}
	}

	return ret

}
