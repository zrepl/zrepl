package zfs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
)

type VersionType string

const (
	Bookmark VersionType = "bookmark"
	Snapshot VersionType = "snapshot"
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

func (t VersionType) String() string {
	return string(t)
}

func DecomposeVersionString(v string) (fs string, versionType VersionType, name string, err error) {
	if len(v) < 3 {
		err = fmt.Errorf("snapshot or bookmark name implausibly short: %s", v)
		return
	}

	snapSplit := strings.SplitN(v, "@", 2)
	bookmarkSplit := strings.SplitN(v, "#", 2)
	if len(snapSplit)*len(bookmarkSplit) != 2 {
		err = fmt.Errorf("dataset cannot be snapshot and bookmark at the same time: %s", v)
		return
	}

	if len(snapSplit) == 2 {
		return snapSplit[0], Snapshot, snapSplit[1], nil
	} else {
		return bookmarkSplit[0], Bookmark, bookmarkSplit[1], nil
	}
}

// The data in a FilesystemVersion is guaranteed to stem from a ZFS CLI invocation.
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

func (v FilesystemVersion) GetCreateTXG() uint64 { return v.CreateTXG }
func (v FilesystemVersion) GetGUID() uint64      { return v.Guid }
func (v FilesystemVersion) GetGuid() uint64      { return v.Guid }
func (v FilesystemVersion) GetName() string      { return v.Name }
func (v FilesystemVersion) IsSnapshot() bool     { return v.Type == Snapshot }
func (v FilesystemVersion) IsBookmark() bool     { return v.Type == Bookmark }
func (v FilesystemVersion) RelName() string {
	return fmt.Sprintf("%s%s", v.Type.DelimiterChar(), v.Name)
}
func (v FilesystemVersion) String() string { return v.RelName() }

func (v FilesystemVersion) ToAbsPath(p *DatasetPath) string {
	var b bytes.Buffer
	b.WriteString(p.ToString())
	b.WriteString(v.Type.DelimiterChar())
	b.WriteString(v.Name)
	return b.String()
}

func (v FilesystemVersion) FullPath(fs string) string {
	return fmt.Sprintf("%s%s", fs, v.RelName())
}

func (v FilesystemVersion) ToSendArgVersion() ZFSSendArgVersion {
	return ZFSSendArgVersion{
		RelName: v.RelName(),
		GUID:    v.Guid,
	}
}

type ParseFilesystemVersionArgs struct {
	fullname                  string
	guid, createtxg, creation string
}

func ParseFilesystemVersion(args ParseFilesystemVersionArgs) (v FilesystemVersion, err error) {
	_, v.Type, v.Name, err = DecomposeVersionString(args.fullname)
	if err != nil {
		return v, err
	}

	if v.Guid, err = strconv.ParseUint(args.guid, 10, 64); err != nil {
		err = errors.Wrapf(err, "cannot parse GUID %q", args.guid)
		return v, err
	}

	if v.CreateTXG, err = strconv.ParseUint(args.createtxg, 10, 64); err != nil {
		err = errors.Wrapf(err, "cannot parse CreateTXG %q", args.createtxg)
		return v, err
	}

	creationUnix, err := strconv.ParseInt(args.creation, 10, 64)
	if err != nil {
		err = errors.Wrapf(err, "cannot parse creation date %q", args.creation)
		return v, err
	} else {
		v.Creation = time.Unix(creationUnix, 0)
	}

	return v, nil
}

type FilesystemVersionFilter interface {
	Filter(t VersionType, name string) (accept bool, err error)
}

type closureFilesystemVersionFilter struct {
	cb func(t VersionType, name string) (accept bool, err error)
}

func (f *closureFilesystemVersionFilter) Filter(t VersionType, name string) (accept bool, err error) {
	return f.cb(t, name)
}

func FilterFromClosure(cb func(t VersionType, name string) (accept bool, err error)) FilesystemVersionFilter {
	return &closureFilesystemVersionFilter{cb}
}

// returned versions are sorted by createtxg
func ZFSListFilesystemVersions(fs *DatasetPath, filter FilesystemVersionFilter) (res []FilesystemVersion, err error) {
	listResults := make(chan ZFSListResult)

	promTimer := prometheus.NewTimer(prom.ZFSListFilesystemVersionDuration.WithLabelValues(fs.ToString()))
	defer promTimer.ObserveDuration()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ZFSListChan(ctx, listResults,
		[]string{"name", "guid", "createtxg", "creation"},
		"-r", "-d", "1",
		"-t", "bookmark,snapshot",
		"-s", "createtxg", fs.ToString())

	res = make([]FilesystemVersion, 0)
	for listResult := range listResults {
		if listResult.Err != nil {
			if listResult.Err == io.ErrUnexpectedEOF {
				// Since we specified the fs on the command line, we'll treat this like the filesystem doesn't exist
				return []FilesystemVersion{}, nil
			}
			return nil, listResult.Err
		}

		line := listResult.Fields
		args := ParseFilesystemVersionArgs{
			fullname:  line[0],
			guid:      line[1],
			createtxg: line[2],
			creation:  line[3],
		}
		v, err := ParseFilesystemVersion(args)
		if err != nil {
			return nil, err
		}

		accept := true
		if filter != nil {
			accept, err = filter.Filter(v.Type, v.Name)
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

func ZFSGetFilesystemVersion(ctx context.Context, ds string) (v FilesystemVersion, _ error) {
	props, err := zfsGet(ctx, ds, []string{"createtxg", "guid", "creation"}, sourceAny)
	if err != nil {
		return v, err
	}
	return ParseFilesystemVersion(ParseFilesystemVersionArgs{
		fullname:  ds,
		createtxg: props.Get("createtxg"),
		guid:      props.Get("guid"),
		creation:  props.Get("creation"),
	})
}
