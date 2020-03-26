package zfs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/zrepl/zrepl/util/envconst"
)

func ZFSDestroyFilesystemVersion(ctx context.Context, filesystem *DatasetPath, version *FilesystemVersion) (err error) {

	datasetPath := version.ToAbsPath(filesystem)

	// Sanity check...
	if !strings.ContainsAny(datasetPath, "@#") {
		return fmt.Errorf("sanity check failed: no @ or # character found in %q", datasetPath)
	}

	return ZFSDestroy(ctx, datasetPath)
}

var destroyerSingleton = destroyerImpl{}

type DestroySnapOp struct {
	Filesystem string
	Name       string
	ErrOut     *error
}

func (o *DestroySnapOp) String() string {
	return fmt.Sprintf("destroy operation %s@%s", o.Filesystem, o.Name)
}

func ZFSDestroyFilesystemVersions(reqs []*DestroySnapOp) {
	doDestroy(context.TODO(), reqs, destroyerSingleton)
}

func setDestroySnapOpErr(b []*DestroySnapOp, err error) {
	for _, r := range b {
		*r.ErrOut = err
	}
}

type destroyer interface {
	Destroy(ctx context.Context, args []string) error
	DestroySnapshotsCommaSyntaxSupported(context.Context) (bool, error)
}

func doDestroy(ctx context.Context, reqs []*DestroySnapOp, e destroyer) {

	var validated []*DestroySnapOp
	for _, req := range reqs {
		// Filesystem and Snapshot should not be empty
		// ZFS will generally fail because those are invalid destroy arguments,
		// but we'd rather apply defensive programming here (doing destroy after all)
		if req.Filesystem == "" {
			*req.ErrOut = fmt.Errorf("Filesystem must not be an empty string")
		} else if req.Name == "" {
			*req.ErrOut = fmt.Errorf("Name must not be an empty string")
		} else {
			validated = append(validated, req)
		}
	}
	reqs = validated

	commaSupported, err := e.DestroySnapshotsCommaSyntaxSupported(ctx)
	if err != nil {
		debug("destroy: comma syntax support detection failed: %s", err)
		setDestroySnapOpErr(reqs, err)
		return
	}

	if !commaSupported {
		doDestroySeq(ctx, reqs, e)
	} else {
		doDestroyBatched(ctx, reqs, e)
	}
}

func doDestroySeq(ctx context.Context, reqs []*DestroySnapOp, e destroyer) {
	for _, r := range reqs {
		*r.ErrOut = e.Destroy(ctx, []string{fmt.Sprintf("%s@%s", r.Filesystem, r.Name)})
	}
}

func doDestroyBatched(ctx context.Context, reqs []*DestroySnapOp, d destroyer) {
	perFS := buildBatches(reqs)
	for _, fsbatch := range perFS {
		doDestroyBatchedRec(ctx, fsbatch, d)
	}
}

func buildBatches(reqs []*DestroySnapOp) [][]*DestroySnapOp {
	if len(reqs) == 0 {
		return nil
	}
	sorted := make([]*DestroySnapOp, len(reqs))
	copy(sorted, reqs)
	sort.SliceStable(sorted, func(i, j int) bool {
		// by filesystem, then snap name
		fscmp := strings.Compare(sorted[i].Filesystem, sorted[j].Filesystem)
		if fscmp != 0 {
			return fscmp == -1
		}
		return strings.Compare(sorted[i].Name, sorted[j].Name) == -1
	})

	// group by fs
	var perFS [][]*DestroySnapOp
	consumed := 0
	for consumed < len(sorted) {
		batchConsumedUntil := consumed
		for ; batchConsumedUntil < len(sorted) && sorted[batchConsumedUntil].Filesystem == sorted[consumed].Filesystem; batchConsumedUntil++ {
		}
		perFS = append(perFS, sorted[consumed:batchConsumedUntil])
		consumed = batchConsumedUntil
	}
	return perFS
}

// batch must be on same Filesystem, panics otherwise
func tryBatch(ctx context.Context, batch []*DestroySnapOp, d destroyer) error {
	if len(batch) == 0 {
		return nil
	}

	batchFS := batch[0].Filesystem
	batchNames := make([]string, len(batch))
	for i := range batchNames {
		batchNames[i] = batch[i].Name
		if batchFS != batch[i].Filesystem {
			panic("inconsistent batch")
		}
	}
	batchArg := fmt.Sprintf("%s@%s", batchFS, strings.Join(batchNames, ","))
	return d.Destroy(ctx, []string{batchArg})
}

// fsbatch must be on same filesystem
func doDestroyBatchedRec(ctx context.Context, fsbatch []*DestroySnapOp, d destroyer) {
	if len(fsbatch) <= 1 {
		doDestroySeq(ctx, fsbatch, d)
		return
	}

	err := tryBatch(ctx, fsbatch, d)
	if err == nil {
		setDestroySnapOpErr(fsbatch, nil)
		return
	}

	if pe, ok := err.(*os.PathError); ok && pe.Err == syscall.E2BIG {
		// see TestExcessiveArgumentsResultInE2BIG
		// try halving batch size, assuming snapshots names are roughly the same length
		debug("batch destroy: E2BIG encountered: %s", err)
		doDestroyBatchedRec(ctx, fsbatch[0:len(fsbatch)/2], d)
		doDestroyBatchedRec(ctx, fsbatch[len(fsbatch)/2:], d)
		return
	}

	singleRun := fsbatch // the destroys that will be tried sequentially after "smart" error handling below

	if err, ok := err.(*DestroySnapshotsError); ok {
		// eliminate undestroyable datasets from batch and try it once again
		strippedBatch, remaining := make([]*DestroySnapOp, 0, len(fsbatch)), make([]*DestroySnapOp, 0, len(fsbatch))

		for _, b := range fsbatch {
			isUndestroyable := false
			for _, undestroyable := range err.Undestroyable {
				if undestroyable == b.Name {
					isUndestroyable = true
					break
				}
			}
			if isUndestroyable {
				remaining = append(remaining, b)
			} else {
				strippedBatch = append(strippedBatch, b)
			}
		}

		err := tryBatch(ctx, strippedBatch, d)
		if err != nil {
			// run entire batch sequentially if the stripped one fails
			// (it shouldn't because we stripped erroneous datasets)
			singleRun = fsbatch // shadow
		} else {
			setDestroySnapOpErr(strippedBatch, nil) // these ones worked
			singleRun = remaining                   // shadow
		}
		// fallthrough
	}

	doDestroySeq(ctx, singleRun, d)

}

type destroyerImpl struct{}

func (d destroyerImpl) Destroy(ctx context.Context, args []string) error {
	if len(args) != 1 {
		// we have no use case for this at the moment, so let's crash (safer than destroying something unexpectedly)
		panic(fmt.Sprintf("unexpected number of arguments: %v", args))
	}
	// we know that we are only using this for snapshots, so also sanity check for an @ in args[0]
	if !strings.ContainsAny(args[0], "@") {
		panic(fmt.Sprintf("sanity check: expecting '@' in call to Destroy, got %q", args[0]))
	}
	return ZFSDestroy(ctx, args[0])
}

var batchDestroyFeatureCheck struct {
	once   sync.Once
	enable bool
	err    error
}

func (d destroyerImpl) DestroySnapshotsCommaSyntaxSupported(ctx context.Context) (bool, error) {
	batchDestroyFeatureCheck.once.Do(func() {
		// "feature discovery"
		cmd := exec.CommandContext(ctx, ZFS_BINARY, "destroy")
		output, err := cmd.CombinedOutput()
		if _, ok := err.(*exec.ExitError); !ok {
			debug("destroy feature check failed: %T %s", err, err)
			batchDestroyFeatureCheck.err = err
		}
		def := strings.Contains(string(output), "<filesystem|volume>@<snap>[%<snap>][,...]")
		batchDestroyFeatureCheck.enable = envconst.Bool("ZREPL_EXPERIMENTAL_ZFS_COMMA_SYNTAX_SUPPORTED", def)
		debug("destroy feature check complete %#v", &batchDestroyFeatureCheck)
	})
	return batchDestroyFeatureCheck.enable, batchDestroyFeatureCheck.err
}
