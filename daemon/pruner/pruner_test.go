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
			Guid: uint64(i),
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

type mockCursor struct {
	snapname string
	guid uint64
}
type mockHistory struct {
	errs map[string][]error
	cursors map[string]*mockCursor
}

func (r *mockHistory) ReplicationCursor(ctx context.Context, req *pdu.ReplicationCursorReq) (*pdu.ReplicationCursorRes, error) {
	fs := req.Filesystem
	if len(r.errs[fs]) > 0 {
		e := r.errs[fs][0]
		r.errs[fs] = r.errs[fs][1:]
		return nil, e
	}
	return &pdu.ReplicationCursorRes{Result: &pdu.ReplicationCursorRes_Guid{Guid: 0}}, nil
}

type stubNetErr struct {
	msg string
	temporary, timeout bool
}

var _ net.Error = stubNetErr{}

func (e stubNetErr) Error() string {
	return e.msg
}

func (e stubNetErr) Temporary() bool { return e.temporary }

func (e stubNetErr) Timeout() bool { return e.timeout }

func TestPruner_Prune(t *testing.T) {

	var _ net.Error = &net.OpError{} // we use it below
	target := &mockTarget{
		listFilesystemsErr: []error{
			stubNetErr{msg: "fakerror0", temporary: true},
		},
		listVersionsErrs: map[string][]error{
			"zroot/foo": {
				stubNetErr{msg: "fakeerror1", temporary: true},
				stubNetErr{msg: "fakeerror2", temporary: true,},
			},
		},
		destroyErrs: map[string][]error{
			"zroot/baz": {
				stubNetErr{msg: "fakeerror3", temporary: true}, // first error puts it back in the queue
				stubNetErr{msg:"permanent error"}, // so it will be last when pruner gives up due to permanent err
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
				stubNetErr{msg: "fakeerror4", temporary: true},
			},
		},
	}

	keepRules := []pruning.KeepRule{pruning.MustKeepRegex("^keep")}

	p := Pruner{
		args: args{
			ctx: WithLogger(context.Background(), logger.NewTestLogger(t)),
			target: target,
			receiver: history,
			rules: keepRules,
			retryWait: 10*time.Millisecond,
		},
		state: Plan,
	}
	p.Prune()

	exp := map[string][]string{
		"zroot/foo": {"drop_c"},
		"zroot/bar": {"drop_g"},
	}

	assert.Equal(t, exp, target.destroyed)

	//assert.Equal(t, map[string][]error{}, target.listVersionsErrs, "retried")

}
