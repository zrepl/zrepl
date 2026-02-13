package platformtest

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/internal/zfs"
)

const (
	PlatformTestPoolName   = "zreplplatformtest"
	PlatformTestImagePath  = "/tmp/zreplplatformtest.pool.img"
	PlatformTestMountpoint = "/tmp/zreplplatformtest.pool"
	PlatformTestImageSize  = 200 * (1 << 20) // 200 MiB
)

var ZpoolExportTimeout time.Duration = 500 * time.Millisecond

func CreateOrReplaceZpool(ctx context.Context, e Execer) error {
	// export pool if it already exists (idempotence)
	if _, err := zfs.ZFSGetRawAnySource(ctx, PlatformTestPoolName, []string{"name"}); err != nil {
		if _, ok := err.(*zfs.DatasetDoesNotExist); ok {
			// we'll create it shortly
		} else {
			return errors.Wrapf(err, "cannot determine whether test pool %q exists", PlatformTestPoolName)
		}
	} else {
		// exists, export it, OpenFile will destroy it
		if err := e.RunExpectSuccessNoOutput(ctx, "zpool", "export", PlatformTestPoolName); err != nil {
			return errors.Wrapf(err, "cannot destroy test pool %q", PlatformTestPoolName)
		}
	}

	// idempotently (re)create the pool image
	image, err := os.OpenFile(PlatformTestImagePath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return errors.Wrap(err, "create image file")
	}
	defer image.Close()
	if err := image.Truncate(PlatformTestImageSize); err != nil {
		return errors.Wrap(err, "create image: truncate")
	}
	image.Close()

	// create the pool (zpool runs via sudo through the interposer,
	// which creates the mountpoint and chmods it to 0777)
	err = e.RunExpectSuccessNoOutput(ctx, "zpool", "create", "-f",
		"-O", fmt.Sprintf("mountpoint=%s", PlatformTestMountpoint),
		PlatformTestPoolName, PlatformTestImagePath,
	)
	if err != nil {
		return errors.Wrap(err, "zpool create")
	}

	return nil
}

func DestroyZpool(ctx context.Context, e Execer) error {

	exportDeadline := time.Now().Add(ZpoolExportTimeout)

	for {
		if time.Now().After(exportDeadline) {
			return errors.Errorf("could not zpool export (got 'pool is busy'): %s", PlatformTestPoolName)
		}
		err := e.RunExpectSuccessNoOutput(ctx, "zpool", "export", PlatformTestPoolName)
		if err == nil {
			break
		}
		if strings.Contains(err.Error(), "pool is busy") {
			runtime.Gosched()
			continue
		}
		if err != nil {
			return errors.Wrapf(err, "export pool %q", PlatformTestPoolName)
		}
	}

	if err := os.Remove(PlatformTestImagePath); err != nil {
		return errors.Wrapf(err, "remove pool image")
	}

	return nil
}
