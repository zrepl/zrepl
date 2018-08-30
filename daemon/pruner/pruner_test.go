package pruner

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/zrepl/zrepl/logger"
	"github.com/zrepl/zrepl/pruning"
	"github.com/zrepl/zrepl/replication/pdu"
	"net"
	"testing"
	"time"
)

type mockFS struct {
	path  string
	snaps []string
}

func (m *mockFS) Filesystem() *pdu.Filesystem {
	return &pdu.Filesystem{
		Path: m.path,
	}
}

func (m *mockFS) FilesystemVersions() []*pdu.FilesystemVersion {
	versions := make([]*pdu.FilesystemVersion, len(m.snaps))
	for i, v := range m.snaps {
		versions[i] = &pdu.FilesystemVersion{
			Type:     pdu.FilesystemVersion_Snapshot,
			Name:     v,
			Creation: pdu.FilesystemVersionCreation(time.Unix(0, 0)),
		}
	}
	return versions
}

type mockTarget struct {
	fss                []mockFS
	destroyed          map[string][]string
	listVersionsErrs   map[string][]error
	listFilesystemsErr []error
	destroyErrs        map[string][]error
}

func (t *mockTarget) ListFilesystems(ctx context.Context) ([]*pdu.Filesystem, error) {
	if len(t.listFilesystemsErr) > 0 {
		e := t.listFilesystemsErr[0]
		t.listFilesystemsErr = t.listFilesystemsErr[1:]
		return nil, e
	}
	fss := make([]*pdu.Filesystem, len(t.fss))
	for i := range fss {
		fss[i] = t.fss[i].Filesystem()
	}
	return fss, nil
}

func (t *mockTarget) ListFilesystemVersions(ctx context.Context, fs string) ([]*pdu.FilesystemVersion, error) {
	if len(t.listVersionsErrs[fs]) != 0 {
		e := t.listVersionsErrs[fs][0]
		t.listVersionsErrs[fs] = t.listVersionsErrs[fs][1:]
		return nil, e
	}

	for _, mfs := range t.fss {
		if mfs.path != fs {
			continue
		}
		return mfs.FilesystemVersions(), nil
	}
	return nil, fmt.Errorf("filesystem %s does not exist", fs)
}

func (t *mockTarget) DestroySnapshots(ctx context.Context, req *pdu.DestroySnapshotsReq) (*pdu.DestroySnapshotsRes, error) {
	fs, snaps := req.Filesystem, req.Snapshots
	if len(t.destroyErrs[fs]) != 0 {
		e := t.destroyErrs[fs][0]
		t.destroyErrs[fs] = t.destroyErrs[fs][1:]
		return nil, e
	}
	destroyed := t.destroyed[fs]
	res := make([]*pdu.DestroySnapshotRes, len(snaps))
	for i, s := range snaps {
		destroyed = append(destroyed, s.Name)
		res[i] = &pdu.DestroySnapshotRes{Error: "", Snapshot: s}
	}
	t.destroyed[fs] = destroyed
	return &pdu.DestroySnapshotsRes{Results: res}, nil
}

type mockHistory struct {
	fss  []mockFS
	errs map[string][]error
}

func (r *mockHistory) WasSnapshotReplicated(ctx context.Context, fs string, version *pdu.FilesystemVersion) (bool, error) {

	if len(r.errs[fs]) > 0 {
		e := r.errs[fs][0]
		r.errs[fs] = r.errs[fs][1:]
		return false, e
	}

	for _, mfs := range r.fss {
		if mfs.path != fs {
			continue
		}
		for _, v := range mfs.FilesystemVersions() {
			if v.Type == version.Type && v.Name == v.Name && v.CreateTXG == version.CreateTXG {
				return true, nil
			}
		}
	}
	return false, nil
}

func TestPruner_Prune(t *testing.T) {

	var _ net.Error = &net.OpError{} // we use it below
	target := &mockTarget{
		listFilesystemsErr: []error{
			&net.OpError{Op: "fakerror0"},
		},
		listVersionsErrs: map[string][]error{
			"zroot/foo": {
				&net.OpError{Op: "fakeerror1"}, // should be classified as temporaty
				&net.OpError{Op: "fakeerror2"},
			},
		},
		destroyErrs: map[string][]error{
			"zroot/foo": {
				fmt.Errorf("permanent error"),
			},
			"zroot/bar": {
				&net.OpError{Op: "fakeerror3"},
			},
		},
		destroyed: make(map[string][]string),
		fss: []mockFS{
			{
				path: "zroot/foo",
				snaps: []string{
					"keep_a",
					"keep_b",
					"drop_c",
					"keep_d",
				},
			},
			{
				path: "zroot/bar",
				snaps: []string{
					"keep_e",
					"keep_f",
					"drop_g",
				},
			},
			{
				path: "zroot/baz",
				snaps: []string{
					"keep_h",
					"drop_i",
				},
			},
		},
	}
	history := &mockHistory{
		errs: map[string][]error{
			"zroot/foo": {
				&net.OpError{Op: "fakeerror4"},
			},
			"zroot/baz": {
				fmt.Errorf("permanent error2"),
			},
		},
	}

	keepRules := []pruning.KeepRule{pruning.MustKeepRegex("^keep")}

	p := NewPruner(10*time.Millisecond, target, history, keepRules)
	ctx := context.Background()
	ctx = WithLogger(ctx, logger.NewTestLogger(t))
	p.Prune(ctx)

	exp := map[string][]string{
		"zroot/bar": {"drop_g"},
		// drop_c is prohibited by failing destroy
		// drop_i is prohibiteed by failing WasSnapshotReplicated call
	}

	assert.Equal(t, exp, target.destroyed)

	//assert.Equal(t, map[string][]error{}, target.listVersionsErrs, "retried")

}
