package zfs

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"fmt"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/zfs/zfscmd"
)

const (
	// For a placeholder filesystem to be a placeholder, the property source must be local,
	// i.e. not inherited.
	PlaceholderPropertyName string = "zrepl:placeholder"
	placeholderPropertyOn   string = "on"
	placeholderPropertyOff  string = "off"
)

// computeLegacyPlaceholderPropertyValue is a legacy-compatibility function.
//
// In the 0.0.x series, the value stored in the PlaceholderPropertyName user property
// was a hash value of the dataset path.
// A simple `on|off` value could not be used at the time because `zfs list` was used to
// list all filesystems and their placeholder state with a single command: due to property
// inheritance, `zfs list` would print the placeholder state for all (non-placeholder) children
// of a dataset, so the hash value was used to distinguish whether the property was local or
// inherited.
//
// One of the drawbacks of the above approach is that `zfs rename` renders a placeholder filesystem
// a non-placeholder filesystem if any of the parent path components change.
//
// We `zfs get` nowadays, which returns the property source, making the hash value no longer
// necessary. However, we want to keep legacy compatibility.
func computeLegacyHashBasedPlaceholderPropertyValue(p *DatasetPath) string {
	ps := []byte(p.ToString())
	sum := sha512.Sum512_256(ps)
	return hex.EncodeToString(sum[:])
}

// the caller asserts that placeholderPropertyValue is sourceLocal
func isLocalPlaceholderPropertyValuePlaceholder(p *DatasetPath, placeholderPropertyValue string) (isPlaceholder bool) {
	legacy := computeLegacyHashBasedPlaceholderPropertyValue(p)
	switch placeholderPropertyValue {
	case legacy:
		return true
	case placeholderPropertyOn:
		return true
	default:
		return false
	}
}

type FilesystemPlaceholderState struct {
	FS                    string
	FSExists              bool
	IsPlaceholder         bool
	RawLocalPropertyValue string
}

// ZFSGetFilesystemPlaceholderState is the authoritative way to determine whether a filesystem
// is a placeholder. Note that the property source must be `local` for the returned value to be valid.
//
// For nonexistent FS, err == nil and state.FSExists == false
func ZFSGetFilesystemPlaceholderState(ctx context.Context, p *DatasetPath) (state *FilesystemPlaceholderState, err error) {
	state = &FilesystemPlaceholderState{FS: p.ToString()}
	state.FS = p.ToString()
	props, err := zfsGet(ctx, p.ToString(), []string{PlaceholderPropertyName}, SourceLocal)
	var _ error = (*DatasetDoesNotExist)(nil) // weak assertion on zfsGet's interface
	if _, ok := err.(*DatasetDoesNotExist); ok {
		return state, nil
	} else if err != nil {
		return state, err
	}
	state.FSExists = true
	state.RawLocalPropertyValue = props.Get(PlaceholderPropertyName)
	state.IsPlaceholder = isLocalPlaceholderPropertyValuePlaceholder(p, state.RawLocalPropertyValue)
	return state, nil
}

//go:generate enumer -type=FilesystemPlaceholderCreateEncryptionValue -trimprefix=FilesystemPlaceholderCreateEncryption
type FilesystemPlaceholderCreateEncryptionValue int

const (
	FilesystemPlaceholderCreateEncryptionInherit FilesystemPlaceholderCreateEncryptionValue = 1 << iota
	FilesystemPlaceholderCreateEncryptionOff
)

func ZFSCreatePlaceholderFilesystem(ctx context.Context, fs *DatasetPath, parent *DatasetPath, encryption FilesystemPlaceholderCreateEncryptionValue) (err error) {
	if fs.Length() == 1 {
		return fmt.Errorf("cannot create %q: pools cannot be created with zfs create", fs.ToString())
	}

	cmdline := []string{
		"create",
		"-o", fmt.Sprintf("%s=%s", PlaceholderPropertyName, placeholderPropertyOn),
		"-o", "mountpoint=none",
	}

	if !encryption.IsAFilesystemPlaceholderCreateEncryptionValue() {
		panic(encryption)
	}
	switch encryption {
	case FilesystemPlaceholderCreateEncryptionInherit:
		// no-op
	case FilesystemPlaceholderCreateEncryptionOff:
		cmdline = append(cmdline, "-o", "encryption=off")
	default:
		panic(encryption)
	}

	cmdline = append(cmdline, fs.ToString())
	cmd := zfscmd.CommandContext(ctx, ZFS_BINARY, cmdline...)

	stdio, err := cmd.CombinedOutput()
	if err != nil {
		err = &ZFSError{
			Stderr:  stdio,
			WaitErr: err,
		}
	}

	return
}

func ZFSSetPlaceholder(ctx context.Context, p *DatasetPath, isPlaceholder bool) error {
	prop := placeholderPropertyOff
	if isPlaceholder {
		prop = placeholderPropertyOn
	}
	props := map[string]string{PlaceholderPropertyName: prop}
	return zfsSet(ctx, p.ToString(), props)
}

type MigrateHashBasedPlaceholderReport struct {
	OriginalState     FilesystemPlaceholderState
	NeedsModification bool
}

// fs must exist, will panic otherwise
func ZFSMigrateHashBasedPlaceholderToCurrent(ctx context.Context, fs *DatasetPath, dryRun bool) (*MigrateHashBasedPlaceholderReport, error) {
	st, err := ZFSGetFilesystemPlaceholderState(ctx, fs)
	if err != nil {
		return nil, fmt.Errorf("error getting placeholder state: %s", err)
	}
	if !st.FSExists {
		panic("inconsistent placeholder state returned: fs must exist")
	}

	report := MigrateHashBasedPlaceholderReport{
		OriginalState: *st,
	}
	report.NeedsModification = st.IsPlaceholder && st.RawLocalPropertyValue != placeholderPropertyOn

	if dryRun || !report.NeedsModification {
		return &report, nil
	}

	err = ZFSSetPlaceholder(ctx, fs, st.IsPlaceholder)
	if err != nil {
		return nil, fmt.Errorf("error re-writing placeholder property: %s", err)
	}
	return &report, nil
}

func ZFSListPlaceholderFilesystemsWithAdditionalProps(ctx context.Context, root string, additionalProps []string) (map[string]*ZFSProperties, error) {

	props := []string{PlaceholderPropertyName}
	if len(additionalProps) > 0 {
		props = append(props, additionalProps...)
	}

	propsByFS, err := zfsGetRecursive(ctx, root, -1, []string{"filesystem", "volume"}, props, SourceAny)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot get placeholder filesystems under %q", root)
	}

	filtered := make(map[string]*ZFSProperties)
	for fs, props := range propsByFS {
		details := props.GetDetails(PlaceholderPropertyName)
		if details.Source != SourceLocal {
			continue
		}
		fsp, err := NewDatasetPath(fs)
		if err != nil {
			return nil, errors.Wrapf(err, "zfs get returned invalid dataset path %q", fs)
		}
		if !isLocalPlaceholderPropertyValuePlaceholder(fsp, details.Value) {
			continue
		}
		filtered[fs] = props
	}

	return filtered, nil
}
