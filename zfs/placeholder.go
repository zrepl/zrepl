package zfs

import (
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"os/exec"
)

const ZREPL_PLACEHOLDER_PROPERTY_NAME string = "zrepl:placeholder"

type FilesystemState struct {
	Placeholder bool
	// TODO extend with resume token when that feature is finally added
}

// A somewhat efficient way to determine if a filesystem exists on this host.
// Particularly useful if exists is called more than once (will only fork exec once and cache the result)
func ZFSListFilesystemState() (localState map[string]FilesystemState, err error) {

	var actual [][]string
	if actual, err = ZFSList([]string{"name", ZREPL_PLACEHOLDER_PROPERTY_NAME}, "-t", "filesystem,volume"); err != nil {
		return
	}

	localState = make(map[string]FilesystemState, len(actual))
	for _, e := range actual {
		dp, err := NewDatasetPath(e[0])
		if err != nil {
			return nil, fmt.Errorf("ZFS does not return parseable dataset path: %s", e[0])
		}
		placeholder, _ := IsPlaceholder(dp, e[1])
		localState[e[0]] = FilesystemState{
			placeholder,
		}
	}
	return

}

// Computes the value for the ZREPL_PLACEHOLDER_PROPERTY_NAME ZFS user property
// to mark the given DatasetPath p as a placeholder
//
// We cannot simply use booleans here since user properties are always
// inherited.
//
// We hash the DatasetPath and use it to check for a given path if it is the
// one originally marked as placeholder.
//
// However, this prohibits moving datasets around via `zfs rename`. The
// placeholder attribute must be re-computed for the dataset path after the
// move.
//
// TODO better solution available?
func PlaceholderPropertyValue(p *DatasetPath) string {
	ps := []byte(p.ToString())
	sum := sha512.Sum512_256(ps)
	return hex.EncodeToString(sum[:])
}

func IsPlaceholder(p *DatasetPath, placeholderPropertyValue string) (isPlaceholder bool, err error) {
	expected := PlaceholderPropertyValue(p)
	isPlaceholder = expected == placeholderPropertyValue
	if !isPlaceholder {
		err = fmt.Errorf("expected %s, has %s", expected, placeholderPropertyValue)
	}
	return
}

// for nonexistent FS, isPlaceholder == false && err == nil
func ZFSIsPlaceholderFilesystem(p *DatasetPath) (isPlaceholder bool, err error) {
	props, err := zfsGet(p.ToString(), []string{ZREPL_PLACEHOLDER_PROPERTY_NAME}, sourceAny)
	if err == io.ErrUnexpectedEOF {
		// interpret this as an early exit of the zfs binary due to the fs not existing
		return false, nil
	} else if err != nil {
		return false, err
	}
	isPlaceholder, _ = IsPlaceholder(p, props.Get(ZREPL_PLACEHOLDER_PROPERTY_NAME))
	return
}

func ZFSCreatePlaceholderFilesystem(p *DatasetPath) (err error) {
	v := PlaceholderPropertyValue(p)
	cmd := exec.Command(ZFS_BINARY, "create",
		"-o", fmt.Sprintf("%s=%s", ZREPL_PLACEHOLDER_PROPERTY_NAME, v),
		"-o", "mountpoint=none",
		p.ToString())

	stderr := bytes.NewBuffer(make([]byte, 0, 1024))
	cmd.Stderr = stderr

	if err = cmd.Start(); err != nil {
		return err
	}

	if err = cmd.Wait(); err != nil {
		err = &ZFSError{
			Stderr:  stderr.Bytes(),
			WaitErr: err,
		}
	}

	return
}

func ZFSSetNoPlaceholder(p *DatasetPath) error {
	props := NewZFSProperties()
	props.Set(ZREPL_PLACEHOLDER_PROPERTY_NAME, "off")
	return zfsSet(p.ToString(), props)
}