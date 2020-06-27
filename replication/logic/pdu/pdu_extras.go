package pdu

import (
	"fmt"
	"time"

	"github.com/zrepl/zrepl/zfs"
)

func (v *FilesystemVersion) GetRelName() string {
	zv, err := v.ZFSFilesystemVersion()
	if err != nil {
		return ""
	}
	return zv.String()
}

func (v *FilesystemVersion) RelName() string {
	zv, err := v.ZFSFilesystemVersion()
	if err != nil {
		panic(err)
	}
	return zv.String()
}

func (v FilesystemVersion_VersionType) ZFSVersionType() zfs.VersionType {
	switch v {
	case FilesystemVersion_Snapshot:
		return zfs.Snapshot
	case FilesystemVersion_Bookmark:
		return zfs.Bookmark
	default:
		panic(fmt.Sprintf("unexpected v.Type %#v", v))
	}
}

func FilesystemVersionFromZFS(fsv *zfs.FilesystemVersion) *FilesystemVersion {
	var t FilesystemVersion_VersionType
	switch fsv.Type {
	case zfs.Bookmark:
		t = FilesystemVersion_Bookmark
	case zfs.Snapshot:
		t = FilesystemVersion_Snapshot
	default:
		panic("unknown fsv.Type: " + fsv.Type)
	}
	return &FilesystemVersion{
		Type:      t,
		Name:      fsv.Name,
		Guid:      fsv.Guid,
		CreateTXG: fsv.CreateTXG,
		Creation:  fsv.Creation.Format(time.RFC3339),
	}
}

func FilesystemVersionCreation(t time.Time) string {
	return t.Format(time.RFC3339)
}

func (v *FilesystemVersion) CreationAsTime() (time.Time, error) {
	return time.Parse(time.RFC3339, v.Creation)
}

// implement fsfsm.FilesystemVersion
func (v *FilesystemVersion) SnapshotTime() time.Time {
	t, err := v.CreationAsTime()
	if err != nil {
		panic(err) // FIXME
	}
	return t
}

func (v *FilesystemVersion) ZFSFilesystemVersion() (*zfs.FilesystemVersion, error) {
	ct, err := v.CreationAsTime()
	if err != nil {
		return nil, err
	}
	return &zfs.FilesystemVersion{
		Type:      v.Type.ZFSVersionType(),
		Name:      v.Name,
		Guid:      v.Guid,
		CreateTXG: v.CreateTXG,
		Creation:  ct,
	}, nil
}

func ReplicationConfigProtectionWithKind(both ReplicationGuaranteeKind) *ReplicationConfigProtection {
	return &ReplicationConfigProtection{
		Initial:     both,
		Incremental: both,
	}
}
