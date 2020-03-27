package endpoint

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/zfs"
)

type ListHoldsAndBookmarksOutputBookmark struct {
	Type AbstractionType
	FS   string
	zfs.FilesystemVersion
	JobID JobID
}

func (b ListHoldsAndBookmarksOutputBookmark) GetType() AbstractionType { return b.Type }
func (b ListHoldsAndBookmarksOutputBookmark) GetFS() string            { return b.FS }
func (b ListHoldsAndBookmarksOutputBookmark) GetJobID() *JobID         { return &b.JobID }
func (b ListHoldsAndBookmarksOutputBookmark) GetFullPath() string {
	return fmt.Sprintf("%s#%s", b.FS, b.Name) // TODO use zfs.FilesystemVersion.ToAbsPath
}
func (b ListHoldsAndBookmarksOutputBookmark) MarshalJSON() ([]byte, error) {
	return json.Marshal(AbstractionJSON{b})
}
func (b ListHoldsAndBookmarksOutputBookmark) String() string {
	return fmt.Sprintf("%s %s", b.Type, b.GetFullPath())
}

func (b ListHoldsAndBookmarksOutputBookmark) GetFilesystemVersion() zfs.FilesystemVersion {
	return b.FilesystemVersion
}

func (b ListHoldsAndBookmarksOutputBookmark) Destroy(ctx context.Context) error {
	if err := zfs.ZFSDestroyIdempotent(ctx, b.GetFullPath()); err != nil {
		return errors.Wrapf(err, "destroy %s: zfs", b)
	}
	return nil
}

type ListHoldsAndBookmarksOutputHold struct {
	Type AbstractionType
	FS   string
	zfs.FilesystemVersion
	Tag   string
	JobID JobID
}

func (h ListHoldsAndBookmarksOutputHold) GetType() AbstractionType { return h.Type }
func (h ListHoldsAndBookmarksOutputHold) GetFS() string            { return h.FS }
func (h ListHoldsAndBookmarksOutputHold) GetJobID() *JobID         { return &h.JobID }
func (h ListHoldsAndBookmarksOutputHold) GetFullPath() string {
	return fmt.Sprintf("%s@%s", h.FS, h.GetName()) // TODO use zfs.FilesystemVersion.ToAbsPath
}
func (h ListHoldsAndBookmarksOutputHold) MarshalJSON() ([]byte, error) {
	return json.Marshal(AbstractionJSON{h})
}
func (h ListHoldsAndBookmarksOutputHold) String() string {
	return fmt.Sprintf("%s %q on %s", h.Type, h.Tag, h.GetFullPath())
}

func (h ListHoldsAndBookmarksOutputHold) GetFilesystemVersion() zfs.FilesystemVersion {
	return h.FilesystemVersion
}

func (h ListHoldsAndBookmarksOutputHold) Destroy(ctx context.Context) error {
	if err := zfs.ZFSRelease(ctx, h.Tag, h.GetFullPath()); err != nil {
		return errors.Wrapf(err, "release %s: zfs", h)
	}
	return nil
}
