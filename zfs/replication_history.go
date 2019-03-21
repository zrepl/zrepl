package zfs

import (
	"fmt"
	"github.com/pkg/errors"
	"strconv"
)

const ReplicationCursorBookmarkName = "zrepl_replication_cursor"

// may return nil for both values, indicating there is no cursor
func ZFSGetReplicationCursor(fs *DatasetPath) (*FilesystemVersion, error) {
	versions, err := ZFSListFilesystemVersions(fs, nil)
	if err != nil {
		return nil, err
	}
	for _, v := range versions {
		if v.Type == Bookmark && v.Name == ReplicationCursorBookmarkName {
			return &v, nil
		}
	}
	return nil, nil
}

func ZFSSetReplicationCursor(fs *DatasetPath, snapname string) (guid uint64, err error) {
	snapPath := fmt.Sprintf("%s@%s", fs.ToString(), snapname)
	debug("replication cursor: snap path %q", snapPath)
	propsSnap, err := zfsGet(snapPath, []string{"createtxg", "guid"}, sourceAny)
	if err != nil {
		return 0, errors.Wrap(err, "zfs: replication cursor: get snapshot createtxg")
	}
	snapGuid, err := strconv.ParseUint(propsSnap.Get("guid"), 10, 64)
	bookmarkPath := fmt.Sprintf("%s#%s", fs.ToString(), ReplicationCursorBookmarkName)
	propsBookmark, err := zfsGet(bookmarkPath, []string{"createtxg"}, sourceAny)
	_, bookmarkNotExistErr := err.(*DatasetDoesNotExist)
	if err != nil && !bookmarkNotExistErr {
		return 0, errors.Wrap(err, "zfs: replication cursor: get bookmark txg")
	}
	if err == nil {
		bookmarkTxg, err := strconv.ParseUint(propsBookmark.Get("createtxg"), 10, 64)
		if err != nil {
			return 0, errors.Wrap(err, "zfs: replication cursor: parse bookmark createtxg")
		}
		snapTxg, err := strconv.ParseUint(propsSnap.Get("createtxg"), 10, 64)
		if err != nil {
			return 0, errors.Wrap(err, "zfs: replication cursor: parse snapshot createtxg")
		}
		if snapTxg < bookmarkTxg {
			return 0, errors.New("zfs: replication cursor: can only be advanced, not set back")
		}
		if err := ZFSDestroy(bookmarkPath); err != nil { // FIXME make safer by using new temporary bookmark, then rename, possible with channel programs
			return 0, errors.Wrap(err, "zfs: replication cursor: destroy current cursor")
		}
	}
	if err := ZFSBookmark(fs, snapname, ReplicationCursorBookmarkName); err != nil {
		return 0, errors.Wrapf(err, "zfs: replication cursor: create bookmark")
	}
	return snapGuid, nil
}
