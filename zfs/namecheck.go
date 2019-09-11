package zfs

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

const MaxDatasetNameLen = 256 - 1

type EntityType string

const (
	EntityTypeFilesystem EntityType = "filesystem"
	EntityTypeVolume     EntityType = "volume"
	EntityTypeSnapshot   EntityType = "snapshot"
	EntityTypeBookmark   EntityType = "bookmark"
)

func (e EntityType) Validate() error {
	switch e {
	case EntityTypeFilesystem:
		return nil
	case EntityTypeVolume:
		return nil
	case EntityTypeSnapshot:
		return nil
	case EntityTypeBookmark:
		return nil
	default:
		return fmt.Errorf("invalid entity type %q", string(e))
	}
}

func (e EntityType) MustValidate() {
	if err := e.Validate(); err != nil {
		panic(err)
	}
}

func (e EntityType) String() string {
	e.MustValidate()
	return string(e)
}

var componentValidChar = regexp.MustCompile(`^[0-9a-zA-Z-_\.: ]+$`)

// From module/zcommon/zfs_namecheck.c
//
// Snapshot names must be made up of alphanumeric characters plus the following
// characters:
//
//	[-_.: ]
//
func ComponentNamecheck(datasetPathComponent string) error {
	if len(datasetPathComponent) == 0 {
		return fmt.Errorf("path component must not be empty")
	}
	if len(datasetPathComponent) > MaxDatasetNameLen {
		return fmt.Errorf("path component must not be longer than %d chars", MaxDatasetNameLen)
	}

	if !(isASCII(datasetPathComponent)) {
		return fmt.Errorf("path component must be ASCII")
	}

	if !componentValidChar.MatchString(datasetPathComponent) {
		return fmt.Errorf("path component must only contain alphanumeric chars and any in %q", "-_.: ")
	}

	if datasetPathComponent == "." || datasetPathComponent == ".." {
		return fmt.Errorf("path component must not be '%s'", datasetPathComponent)
	}

	return nil
}

type PathValidationError struct {
	path       string
	entityType EntityType
	msg        string
}

func (e *PathValidationError) Path() string { return e.path }

func (e *PathValidationError) Error() string {
	return fmt.Sprintf("invalid %s %q: %s", e.entityType, e.path, e.msg)
}

// combines
//
//    lib/libzfs/libzfs_dataset.c: zfs_validate_name
//    module/zcommon/zfs_namecheck.c: entity_namecheck
//
// The '%' character is not allowed because it's reserved for zfs-internal use
func EntityNamecheck(path string, t EntityType) (err *PathValidationError) {
	pve := func(msg string) *PathValidationError {
		return &PathValidationError{path: path, entityType: t, msg: msg}
	}

	t.MustValidate()

	// delimiter checks
	if t != EntityTypeSnapshot && strings.Contains(path, "@") {
		return pve("snapshot delimiter '@' is not expected here")
	}
	if t == EntityTypeSnapshot && !strings.Contains(path, "@") {
		return pve("missing '@' delimiter in snapshot name")
	}
	if t != EntityTypeBookmark && strings.Contains(path, "#") {
		return pve("bookmark delimiter '#' is not expected here")
	}
	if t == EntityTypeBookmark && !strings.Contains(path, "#") {
		return pve("missing '#' delimiter in bookmark name")
	}

	// EntityTypeVolume and EntityTypeFilesystem are already covered above

	if strings.Contains(path, "%") {
		return pve("invalid character '%' in name")
	}

	// mimic module/zcommon/zfs_namecheck.c: entity_namecheck

	if len(path) > MaxDatasetNameLen {
		return pve("name too long")
	}

	if len(path) == 0 {
		return pve("must not be empty")
	}

	if !isASCII(path) {
		return pve("must be ASCII")
	}

	slashComps := bytes.Split([]byte(path), []byte("/"))
	bookmarkOrSnapshotDelims := 0
	for compI, comp := range slashComps {

		snapCount := bytes.Count(comp, []byte("@"))
		bookCount := bytes.Count(comp, []byte("#"))

		if !(snapCount*bookCount == 0) {
			panic("implementation error: delimiter checks before this loop must ensure this cannot happen")
		}
		bookmarkOrSnapshotDelims += snapCount + bookCount
		if bookmarkOrSnapshotDelims > 1 {
			return pve("multiple delimiters '@' or '#' are not allowed")
		}
		if bookmarkOrSnapshotDelims == 1 && compI != len(slashComps)-1 {
			return pve("snapshot or bookmark must not contain '/'")
		}

		if bookmarkOrSnapshotDelims == 0 {
			// hot path, all but last component
			if err := ComponentNamecheck(string(comp)); err != nil {
				return pve(err.Error())
			}
			continue
		}

		subComps := bytes.FieldsFunc(comp, func(r rune) bool {
			return r == '#' || r == '@'
		})
		if len(subComps) > 2 {
			panic("implementation error: delimiter checks above should ensure a single bookmark or snapshot delimiter per component")
		}
		if len(subComps) != 2 {
			return pve("empty component, bookmark or snapshot name not allowed")
		}

		for _, comp := range subComps {
			if err := ComponentNamecheck(string(comp)); err != nil {
				return pve(err.Error())
			}
		}

	}

	return nil
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > unicode.MaxASCII {
			return false
		}
	}
	return true
}
