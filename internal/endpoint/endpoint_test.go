package endpoint

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/internal/pruning"
	"github.com/zrepl/zrepl/internal/replication/logic/pdu"
)

type fakeKeepSnapshot struct {
	name string
	date time.Time
}

func (s fakeKeepSnapshot) Name() string     { return s.name }
func (s fakeKeepSnapshot) Replicated() bool { return true }
func (s fakeKeepSnapshot) Date() time.Time  { return s.date }

func TestPartitionByLocalPruningPolicy(t *testing.T) {
	base := time.Unix(1000000, 0)
	local := []pruning.Snapshot{
		fakeKeepSnapshot{"zrepl_1", base.Add(1 * time.Hour)},
		fakeKeepSnapshot{"zrepl_2", base.Add(2 * time.Hour)},
		fakeKeepSnapshot{"zrepl_3", base.Add(3 * time.Hour)},
		fakeKeepSnapshot{"manual_1", base.Add(4 * time.Hour)},
	}

	names := func(fsvs []*pdu.FilesystemVersion) (ns []string) {
		for _, fsv := range fsvs {
			ns = append(ns, fsv.Name)
		}
		return ns
	}
	protectedNames := func(ress []*pdu.DestroySnapshotRes) (ns []string) {
		for _, res := range ress {
			ns = append(ns, res.Snapshot.Name)
		}
		return ns
	}
	request := func(ns ...string) (fsvs []*pdu.FilesystemVersion) {
		for _, n := range ns {
			fsvs = append(fsvs, &pdu.FilesystemVersion{
				Type: pdu.FilesystemVersion_Snapshot,
				Name: n,
			})
		}
		return fsvs
	}

	keepZrepl, err := pruning.NewKeepRegex("^zrepl_.*", false)
	require.NoError(t, err)
	keepLast1, err := pruning.NewKeepLastN(1, "^zrepl_.*")
	require.NoError(t, err)

	t.Run("protected snapshots are retained and reported as success", func(t *testing.T) {
		allowed, protected := partitionByLocalPruningPolicy(
			request("zrepl_1", "manual_1"), local, []pruning.KeepRule{keepZrepl})
		assert.Equal(t, []string{"manual_1"}, names(allowed))
		assert.Equal(t, []string{"zrepl_1"}, protectedNames(protected))
		for _, p := range protected {
			assert.Empty(t, p.Error, "protected snapshots must be reported without error")
		}
	})

	t.Run("destroy-eligible snapshots pass through", func(t *testing.T) {
		// keepLast1 keeps only the most recent zrepl_ snapshot (zrepl_3)
		allowed, protected := partitionByLocalPruningPolicy(
			request("zrepl_1", "zrepl_2", "zrepl_3"), local, []pruning.KeepRule{keepLast1})
		assert.Equal(t, []string{"zrepl_1", "zrepl_2"}, names(allowed))
		assert.Equal(t, []string{"zrepl_3"}, protectedNames(protected))
	})

	t.Run("snapshots not present locally are not misclassified as protected", func(t *testing.T) {
		allowed, protected := partitionByLocalPruningPolicy(
			request("zrepl_gone"), local, []pruning.KeepRule{keepZrepl})
		assert.Equal(t, []string{"zrepl_gone"}, names(allowed))
		assert.Empty(t, protected)
	})

	t.Run("every requested snapshot appears in exactly one partition", func(t *testing.T) {
		req := request("zrepl_1", "zrepl_2", "zrepl_3", "manual_1", "zrepl_gone")
		allowed, protected := partitionByLocalPruningPolicy(
			req, local, []pruning.KeepRule{keepZrepl})
		assert.Len(t, append(names(allowed), protectedNames(protected)...), len(req))
	})

	t.Run("zero rules would protect everything", func(t *testing.T) {
		// DestroySnapshots must guard against this with its
		// len(conf.Keep) == 0 fast path
		allowed, protected := partitionByLocalPruningPolicy(
			request("zrepl_1"), local, nil)
		assert.Empty(t, allowed)
		assert.Equal(t, []string{"zrepl_1"}, protectedNames(protected))
	})
}
