package zfs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type VersionType string

const (
	Bookmark VersionType = "bookmark"
	Snapshot             = "snapshot"
)

func (t VersionType) DelimiterChar() string {
	switch t {
	case Bookmark:
		return "#"
	case Snapshot:
		return "@"
	default:
		panic(fmt.Sprintf("unexpected VersionType %#v", t))
	}
}

type FilesystemVersion struct {
	Type VersionType

	// Display name. Should not be used for identification, only for user output
	Name string

	// GUID as exported by ZFS. Uniquely identifies a snapshot across pools
	Guid uint64

	// The TXG in which the snapshot was created. For bookmarks,
	// this is the GUID of the snapshot it was initially tied to.
	CreateTXG uint64

	// The time the dataset was created
	Creation time.Time
}

func (v FilesystemVersion) String() string {
	return fmt.Sprintf("%s%s", v.Type.DelimiterChar(), v.Name)
}

func (v FilesystemVersion) ToAbsPath(p *DatasetPath) string {
	var b bytes.Buffer
	b.WriteString(p.ToString())
	b.WriteString(v.Type.DelimiterChar())
	b.WriteString(v.Name)
	return b.String()
}

type FilesystemVersionFilter interface {
	Filter(fsv FilesystemVersion) (accept bool, err error)
}

func ZFSListFilesystemVersions(fs *DatasetPath, filter FilesystemVersionFilter) (res []FilesystemVersion, err error) {
	listResults := make(chan ZFSListResult)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ZFSListChan(ctx, listResults,
		[]string{"name", "guid", "createtxg", "creation"},
		"-r", "-d", "1",
		"-t", "bookmark,snapshot",
		"-s", "createtxg", fs.ToString())

	res = make([]FilesystemVersion, 0)
	for listResult := range listResults {
		if listResult.err != nil {
			return nil, listResult.err
		}

		line := listResult.fields

		if len(line[0]) < 3 {
			err = errors.New(fmt.Sprintf("snapshot or bookmark name implausibly short: %s", line[0]))
			return
		}

		snapSplit := strings.SplitN(line[0], "@", 2)
		bookmarkSplit := strings.SplitN(line[0], "#", 2)
		if len(snapSplit)*len(bookmarkSplit) != 2 {
			err = errors.New(fmt.Sprintf("dataset cannot be snapshot and bookmark at the same time: %s", line[0]))
			return
		}

		var v FilesystemVersion
		if len(snapSplit) == 2 {
			v.Name = snapSplit[1]
			v.Type = Snapshot
		} else {
			v.Name = bookmarkSplit[1]
			v.Type = Bookmark
		}

		if v.Guid, err = strconv.ParseUint(line[1], 10, 64); err != nil {
			err = errors.New(fmt.Sprintf("cannot parse GUID: %s", err.Error()))
			return
		}

		if v.CreateTXG, err = strconv.ParseUint(line[2], 10, 64); err != nil {
			err = errors.New(fmt.Sprintf("cannot parse CreateTXG: %s", err.Error()))
			return
		}

		creationUnix, err := strconv.ParseInt(line[3], 10, 64)
		if err != nil {
			err = fmt.Errorf("cannot parse creation date '%s': %s", line[3], err)
			return nil, err
		} else {
			v.Creation = time.Unix(creationUnix, 0)
		}

		accept := true
		if filter != nil {
			accept, err = filter.Filter(v)
			if err != nil {
				err = fmt.Errorf("error executing filter: %s", err)
				return nil, err
			}
		}
		if accept {
			res = append(res, v)
		}

	}
	return
}

func ZFSDestroyFilesystemVersion(filesystem *DatasetPath, version FilesystemVersion) (err error) {

	datasetPath := version.ToAbsPath(filesystem)

	// Sanity check...
	if strings.IndexAny(datasetPath, "@#") == -1 {
		return fmt.Errorf("sanity check failed: no @ character found in dataset path: %s", datasetPath)
	}

	err = ZFSDestroy(datasetPath)
	if err == nil {
		return
	}

	// Check for EBUSY, special meaning to us
	return

}
