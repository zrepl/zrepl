package hooks

import (
	"fmt"

	"github.com/zrepl/zrepl/config"
	"github.com/zrepl/zrepl/zfs"
)

type List []Hook

func HookFromConfig(in config.HookEnum) (Hook, error) {
	switch v := in.Ret.(type) {
	case *config.HookCommand:
		return NewCommandHook(v)
	case *config.HookPostgresCheckpoint:
		return PgChkptHookFromConfig(v)
	case *config.HookMySQLLockTables:
		return MyLockTablesFromConfig(v)
	default:
		return nil, fmt.Errorf("unknown hook type %T", v)
	}
}

func ListFromConfig(in *config.HookList) (r *List, err error) {
	hl := make(List, len(*in))

	for i, h := range *in {
		hl[i], err = HookFromConfig(h)
		if err != nil {
			return nil, fmt.Errorf("create hook #%d: %s", i+1, err)
		}
	}

	return &hl, nil
}

func (l List) CopyFilteredForFilesystem(fs *zfs.DatasetPath) (ret List, err error) {
	ret = make(List, 0, len(l))

	for _, h := range l {
		var passFilesystem bool
		if passFilesystem, err = h.Filesystems().Filter(fs); err != nil {
			return nil, err
		}
		if passFilesystem {
			ret = append(ret, h)
		}
	}

	return ret, nil
}
