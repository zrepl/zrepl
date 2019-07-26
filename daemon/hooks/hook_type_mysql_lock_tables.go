package hooks

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	sqldriver "database/sql/driver"

	"github.com/go-sql-driver/mysql"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/daemon/filters"
	"github.com/zrepl/zrepl/zfs"
)

// https://dev.mysql.com/doc/mysql-backup-excerpt/5.7/en/backup-methods.html
//
// Making Backups Using a File System Snapshot:
//
//  If you are using a Veritas file system, you can make a backup like this:
//
//    	From a client program, execute FLUSH TABLES WITH READ LOCK.
//    	From another shell, execute mount vxfs snapshot.
//    	From the first client, execute UNLOCK TABLES.
//    	Copy files from the snapshot.
//    	Unmount the snapshot.
//
//	Similar snapshot capabilities may be available in other file systems, such as LVM or ZFS.
type MySQLLockTables struct {
	errIsFatal  bool
	connector   sqldriver.Connector
	filesystems Filter
}

type myLockTablesStateKey int

const (
	myLockTablesConnection myLockTablesStateKey = 1 + iota
)

func MyLockTablesFromConfig(in *config.HookMySQLLockTables) (*MySQLLockTables, error) {
	conf, err := mysql.ParseDSN(in.DSN)
	if err != nil {
		return nil, errors.Wrap(err, "`dsn` invalid")
	}
	cn, err := mysql.NewConnector(conf)
	if err != nil {
		return nil, errors.Wrap(err, "`connect` invalid")
	}

	filesystems, err := filters.DatasetMapFilterFromConfig(in.Filesystems)
	if err != nil {
		return nil, errors.Wrap(err, "`filesystems` invalid")
	}

	return &MySQLLockTables{
		in.ErrIsFatal,
		cn,
		filesystems,
	}, nil
}

func (h *MySQLLockTables) ErrIsFatal() bool    { return h.errIsFatal }
func (h *MySQLLockTables) Filesystems() Filter { return h.filesystems }
func (h *MySQLLockTables) String() string      { return "MySQL FLUSH TABLES WITH READ LOCK" }

type MyLockTablesReport struct {
	What string
	Err  error
}

func (r *MyLockTablesReport) HadError() bool { return r.Err != nil }
func (r *MyLockTablesReport) Error() string  { return r.String() }
func (r *MyLockTablesReport) String() string {
	var s strings.Builder
	s.WriteString(r.What)
	if r.Err != nil {
		fmt.Fprintf(&s, ": %s", r.Err)
	}
	return s.String()
}

func (h *MySQLLockTables) Run(ctx context.Context, edge Edge, phase Phase, dryRun bool, extra Env, state map[interface{}]interface{}) HookReport {
	fs, ok := extra[EnvFS]
	if !ok {
		panic(extra)
	}
	dp, err := zfs.NewDatasetPath(fs)
	if err != nil {
		panic(err)
	}
	if pass, err := h.filesystems.Filter(dp); err != nil {
		return &MyLockTablesReport{What: "filesystem filter", Err: err}
	} else if !pass {
		getLogger(ctx).Debug("filesystem does not match filter, skipping")
		return &MyLockTablesReport{What: "filesystem filter skipped this filesystem", Err: nil}
	}

	switch edge {
	case Pre:
		err := h.doRunPre(ctx, dp, dryRun, state)
		return &MyLockTablesReport{"FLUSH TABLES WITH READ LOCK", err}
	case Post:
		err := h.doRunPost(ctx, dp, dryRun, state)
		return &MyLockTablesReport{"UNLOCK TABLES", err}
	}
	return &MyLockTablesReport{What: "skipped this edge", Err: nil}
}

func (h *MySQLLockTables) doRunPre(ctx context.Context, fs *zfs.DatasetPath, dry bool, state map[interface{}]interface{}) (err error) {
	db := sql.OpenDB(h.connector)
	defer func(err *error) {
		if *err != nil {
			db.Close()
		}
	}(&err)

	getLogger(ctx).Debug("do FLUSH TABLES WITH READ LOCK")
	_, err = db.ExecContext(ctx, "FLUSH TABLES WITH READ LOCK")
	if err != nil {
		return
	}

	state[myLockTablesConnection] = db

	return nil
}

func (h *MySQLLockTables) doRunPost(ctx context.Context, fs *zfs.DatasetPath, dry bool, state map[interface{}]interface{}) error {

	db := state[myLockTablesConnection].(*sql.DB)
	defer db.Close()

	getLogger(ctx).Debug("do UNLOCK TABLES")
	_, err := db.ExecContext(ctx, "UNLOCK TABLES")
	if err != nil {
		return err
	}

	return nil
}
