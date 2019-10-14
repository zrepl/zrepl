package platformtest

import (
	"context"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/zfs"
)

type Zpool struct {
	args ZpoolCreateArgs
}

type ZpoolCreateArgs struct {
	PoolName   string
	ImagePath  string
	ImageSize  int64
	Mountpoint string
}

func (a ZpoolCreateArgs) Validate() error {
	if !filepath.IsAbs(a.ImagePath) {
		return errors.Errorf("ImagePath must be absolute, got %q", a.ImagePath)
	}
	const minImageSize = 1024
	if a.ImageSize < minImageSize {
		return errors.Errorf("ImageSize must be > %v, got %v", minImageSize, a.ImageSize)
	}
	if a.Mountpoint != "none" {
		return errors.Errorf("Mountpoint must be \"none\"")
	}
	if a.PoolName == "" {
		return errors.Errorf("PoolName must not be emtpy")
	}
	return nil
}

func CreateOrReplaceZpool(ctx context.Context, e Execer, args ZpoolCreateArgs) (*Zpool, error) {
	if err := args.Validate(); err != nil {
		return nil, errors.Wrap(err, "zpool create args validation error")
	}

	// export pool if it already exists (idempotence)
	if _, err := zfs.ZFSGetRawAnySource(args.PoolName, []string{"name"}); err != nil {
		if _, ok := err.(*zfs.DatasetDoesNotExist); ok {
			// we'll create it shortly
		} else {
			return nil, errors.Wrapf(err, "cannot determine whether test pool %q exists", args.PoolName)
		}
	} else {
		// exists, export it, OpenFile will destroy it
		if err := e.RunExpectSuccessNoOutput(ctx, "zpool", "export", args.PoolName); err != nil {
			return nil, errors.Wrapf(err, "cannot destroy test pool %q", args.PoolName)
		}
	}

	// idempotently (re)create the pool image
	image, err := os.OpenFile(args.ImagePath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, errors.Wrap(err, "create image file")
	}
	defer image.Close()
	if err := image.Truncate(args.ImageSize); err != nil {
		return nil, errors.Wrap(err, "create image: truncate")
	}
	image.Close()

	// create the pool
	err = e.RunExpectSuccessNoOutput(ctx, "zpool", "create", "-f", "-O", "mountpoint=none", args.PoolName, args.ImagePath)
	if err != nil {
		return nil, errors.Wrap(err, "zpool create")
	}

	return &Zpool{args}, nil
}

func (p *Zpool) Name() string { return p.args.PoolName }

func (p *Zpool) Destroy(ctx context.Context, e Execer) error {

	if err := e.RunExpectSuccessNoOutput(ctx, "zpool", "export", p.args.PoolName); err != nil {
		return errors.Wrapf(err, "export pool %q", p.args.PoolName)
	}

	if err := os.Remove(p.args.ImagePath); err != nil {
		return errors.Wrapf(err, "remove pool image")
	}

	return nil
}
