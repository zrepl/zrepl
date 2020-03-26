package endpoint

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/zfs"
)

type bookmarkBasedAbstraction struct {
	Type AbstractionType
	FS   string
	zfs.FilesystemVersion
	JobID JobID
}

func (b bookmarkBasedAbstraction) GetType() AbstractionType { return b.Type }
func (b bookmarkBasedAbstraction) GetFS() string            { return b.FS }
func (b bookmarkBasedAbstraction) GetJobID() *JobID         { return &b.JobID }
func (b bookmarkBasedAbstraction) GetFullPath() string {
	return fmt.Sprintf("%s#%s", b.FS, b.Name) // TODO use zfs.FilesystemVersion.ToAbsPath
}
func (b bookmarkBasedAbstraction) MarshalJSON() ([]byte, error) {
	return json.Marshal(AbstractionJSON{b})
}
func (b bookmarkBasedAbstraction) String() string {
	return fmt.Sprintf("%s %s", b.Type, b.GetFullPath())
}

func (b bookmarkBasedAbstraction) GetFilesystemVersion() zfs.FilesystemVersion {
	return b.FilesystemVersion
}

func (b bookmarkBasedAbstraction) Destroy(ctx context.Context) error {
	if err := zfs.ZFSDestroyIdempotent(ctx, b.GetFullPath()); err != nil {
		return errors.Wrapf(err, "destroy %s: zfs", b)
	}
	return nil
}

type holdBasedAbstraction struct {
	Type AbstractionType
	FS   string
	zfs.FilesystemVersion
	Tag   string
	JobID JobID
}

func (h holdBasedAbstraction) GetType() AbstractionType { return h.Type }
func (h holdBasedAbstraction) GetFS() string            { return h.FS }
func (h holdBasedAbstraction) GetJobID() *JobID         { return &h.JobID }
func (h holdBasedAbstraction) GetFullPath() string {
	return fmt.Sprintf("%s@%s", h.FS, h.GetName()) // TODO use zfs.FilesystemVersion.ToAbsPath
}
func (h holdBasedAbstraction) MarshalJSON() ([]byte, error) {
	return json.Marshal(AbstractionJSON{h})
}
func (h holdBasedAbstraction) String() string {
	return fmt.Sprintf("%s %q on %s", h.Type, h.Tag, h.GetFullPath())
}

func (h holdBasedAbstraction) GetFilesystemVersion() zfs.FilesystemVersion {
	return h.FilesystemVersion
}

func (h holdBasedAbstraction) Destroy(ctx context.Context) error {
	if err := zfs.ZFSRelease(ctx, h.Tag, h.GetFullPath()); err != nil {
		return errors.Wrapf(err, "release %s: zfs", h)
	}
	return nil
}
