package main

import (
	"fmt"
	"github.com/zrepl/zrepl/zfs"
	"time"
)

type AutosnapContext struct {
	Autosnap Autosnap
}

func doAutosnap(ctx AutosnapContext, log Logger) (err error) {

	snap := ctx.Autosnap

	filesystems, err := zfs.ZFSListMapping(snap.DatasetFilter)
	if err != nil {
		return fmt.Errorf("cannot filter datasets: %s", err)
	}

	suffix := time.Now().In(time.UTC).Format("20060102_150405_000")
	snapname := fmt.Sprintf("%s%s", snap.Prefix, suffix)

	hadError := false

	for _, fs := range filesystems { // optimization: use recursive snapshots / channel programs here
		log.Printf("snapshotting filesystem %s@%s", fs, snapname)
		err := zfs.ZFSSnapshot(fs, snapname, false)
		if err != nil {
			log.Printf("error snapshotting %s: %s", fs, err)
			hadError = true
		}
	}

	if hadError {
		err = fmt.Errorf("errors occurred during autosnap, check logs for details")
	}

	return

}
