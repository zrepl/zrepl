package hooks

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/internal/config"
	"github.com/zrepl/zrepl/internal/daemon/filters"
	"github.com/zrepl/zrepl/internal/zfs"
)

type PgChkptHook struct {
	errIsFatal  bool
	connector   *pq.Connector
	filesystems Filter
}

func PgChkptHookFromConfig(in *config.HookPostgresCheckpoint) (*PgChkptHook, error) {
	filesystems, err := filters.DatasetMapFilterFromConfig(in.Filesystems)
	if err != nil {
		return nil, errors.Wrap(err, "`filesystems` invalid")
	}
	cn, err := pq.NewConnector(in.DSN)
	if err != nil {
		return nil, errors.Wrap(err, "`dsn` invalid")
	}

	return &PgChkptHook{
		in.ErrIsFatal,
		cn,
		filesystems,
	}, nil
}

func (h *PgChkptHook) ErrIsFatal() bool    { return h.errIsFatal }
func (h *PgChkptHook) Filesystems() Filter { return h.filesystems }
func (h *PgChkptHook) String() string      { return "postgres checkpoint" }

type PgChkptHookReport struct{ Err error }

func (r *PgChkptHookReport) HadError() bool { return r.Err != nil }
func (r *PgChkptHookReport) Error() string  { return r.Err.Error() }
func (r *PgChkptHookReport) String() string {
	if r.Err != nil {
		return fmt.Sprintf("postgres CHECKPOINT failed: %s", r.Err)
	} else {
		return "postgres CHECKPOINT completed"
	}
}

func (h *PgChkptHook) Run(ctx context.Context, edge Edge, phase Phase, dryRun bool, extra Env, state map[interface{}]interface{}) HookReport {
	if edge != Pre {
		return &PgChkptHookReport{nil}
	}
	fs, ok := extra[EnvFS]
	if !ok {
		panic(extra)
	}
	dp, err := zfs.NewDatasetPath(fs)
	if err != nil {
		panic(err)
	}
	err = h.doRunPre(ctx, dp, dryRun)
	return &PgChkptHookReport{err}
}

func (h *PgChkptHook) doRunPre(ctx context.Context, fs *zfs.DatasetPath, dry bool) error {

	if pass, err := h.filesystems.Filter(fs); err != nil || !pass {
		getLogger(ctx).Debug("filesystem does not match filter, skipping")
		return err
	}

	db := sql.OpenDB(h.connector)
	defer db.Close()
	dl, ok := ctx.Deadline()
	if ok {
		timeout := uint64(math.Floor(time.Until(dl).Seconds() * 1000)) // TODO go1.13 milliseconds
		getLogger(ctx).WithField("statement_timeout", timeout).Debug("setting statement timeout for CHECKPOINT")
		_, err := db.ExecContext(ctx, "SET statement_timeout TO ?", timeout)
		if err != nil {
			return err
		}
	}
	if dry {
		getLogger(ctx).Debug("dry-run - use ping instead of CHECKPOINT")
		return db.PingContext(ctx)
	}
	getLogger(ctx).Info("execute CHECKPOINT command")
	_, err := db.ExecContext(ctx, "CHECKPOINT")
	return err
}
