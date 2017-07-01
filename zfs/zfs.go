package zfs

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"github.com/zrepl/zrepl/util"
	"io"
	"os/exec"
	"strings"
)

type DatasetPath []string

func (p DatasetPath) ToString() string {
	return strings.Join(p, "/")
}

func (p DatasetPath) Empty() bool {
	return len(p) == 0
}

var EmptyDatasetPath DatasetPath = []string{}

func NewDatasetPath(s string) (p DatasetPath, err error) {
	if s == "" {
		return EmptyDatasetPath, nil // the empty dataset path
	}
	const FORBIDDEN = "@#|\t "
	if strings.ContainsAny(s, FORBIDDEN) { // TODO space may be a bit too restrictive...
		return nil, errors.New(fmt.Sprintf("path '%s' contains forbidden characters (any of '%s')", s, FORBIDDEN))
	}
	return strings.Split(s, "/"), nil
}

func toDatasetPath(s string) DatasetPath {
	p, err := NewDatasetPath(s)
	if err != nil {
		panic(err)
	}
	return p
}

type DatasetFilter func(path DatasetPath) bool

type ZFSError struct {
	Stderr  []byte
	WaitErr error
}

func (e ZFSError) Error() string {
	return fmt.Sprintf("zfs exited with error: %s", e.WaitErr.Error())
}

var ZFS_BINARY string = "zfs"

func ZFSList(properties []string, zfsArgs ...string) (res [][]string, err error) {

	args := make([]string, 0, 4+len(zfsArgs))
	args = append(args,
		"list", "-H", "-p",
		"-o", strings.Join(properties, ","))
	args = append(args, zfsArgs...)

	cmd := exec.Command(ZFS_BINARY, args...)

	var stdout io.Reader
	stderr := bytes.NewBuffer(make([]byte, 0, 1024))
	cmd.Stderr = stderr

	if stdout, err = cmd.StdoutPipe(); err != nil {
		return
	}

	if err = cmd.Start(); err != nil {
		return
	}

	s := bufio.NewScanner(stdout)
	buf := make([]byte, 1024)
	s.Buffer(buf, 0)

	res = make([][]string, 0)

	for s.Scan() {
		fields := strings.SplitN(s.Text(), "\t", len(properties))

		if len(fields) != len(properties) {
			err = errors.New("unexpected output")
			return
		}

		res = append(res, fields)
	}

	if waitErr := cmd.Wait(); waitErr != nil {
		err := ZFSError{
			Stderr:  stderr.Bytes(),
			WaitErr: waitErr,
		}
		return nil, err
	}
	return
}

func ZFSSend(fs DatasetPath, from, to *FilesystemVersion) (stream io.Reader, err error) {

	args := make([]string, 0)
	args = append(args, "send")

	if to == nil { // Initial
		args = append(args, from.ToAbsPath(fs))
	} else {
		args = append(args, "-i", from.ToAbsPath(fs), to.ToAbsPath(fs))
	}

	stream, err = util.RunIOCommand(ZFS_BINARY, args...)

	return
}

func ZFSRecv(fs DatasetPath, stream io.Reader, additionalArgs ...string) (err error) {

	args := make([]string, 0)
	args = append(args, "recv")
	if len(args) > 0 {
		args = append(args, additionalArgs...)
	}
	args = append(args, fs.ToString())

	cmd := exec.Command(ZFS_BINARY, args...)

	stderr := bytes.NewBuffer(make([]byte, 0, 1024))
	cmd.Stderr = stderr

	// TODO report bug upstream
	// Setup an unused stdout buffer.
	// Otherwise, ZoL v0.6.5.9-1 3.16.0-4-amd64 writes the following error to stderr and exits with code 1
	//   cannot receive new filesystem stream: invalid backup stream
	stdout := bytes.NewBuffer(make([]byte, 0, 1024))
	cmd.Stdout = stdout

	cmd.Stdin = stream

	if err = cmd.Start(); err != nil {
		return
	}

	if err = cmd.Wait(); err != nil {
		err = ZFSError{
			Stderr:  stderr.Bytes(),
			WaitErr: err,
		}
		return
	}

	return nil
}

func ZFSSet(fs DatasetPath, prop, val string) (err error) {

	if strings.ContainsRune(prop, '=') {
		panic("prop contains rune '=' which is the delimiter between property name and value")
	}

	cmd := exec.Command(ZFS_BINARY, "set", fmt.Sprintf("%s=%s", prop, val), fs.ToString())

	stderr := bytes.NewBuffer(make([]byte, 0, 1024))
	cmd.Stderr = stderr

	if err = cmd.Start(); err != nil {
		return err
	}

	if err = cmd.Wait(); err != nil {
		err = ZFSError{
			Stderr:  stderr.Bytes(),
			WaitErr: err,
		}
	}

	return
}

func ZFSDestroy(dataset string) (err error) {

	cmd := exec.Command(ZFS_BINARY, "destroy", dataset)

	stderr := bytes.NewBuffer(make([]byte, 0, 1024))
	cmd.Stderr = stderr

	if err = cmd.Start(); err != nil {
		return err
	}

	if err = cmd.Wait(); err != nil {
		err = ZFSError{
			Stderr:  stderr.Bytes(),
			WaitErr: err,
		}
	}

	return

}

func ZFSSnapshot(fs DatasetPath, name string, recursive bool) (err error) {

	snapname := fmt.Sprintf("%s@%s", fs.ToString(), name)
	cmd := exec.Command(ZFS_BINARY, "snapshot", snapname)

	stderr := bytes.NewBuffer(make([]byte, 0, 1024))
	cmd.Stderr = stderr

	if err = cmd.Start(); err != nil {
		return err
	}

	if err = cmd.Wait(); err != nil {
		err = ZFSError{
			Stderr:  stderr.Bytes(),
			WaitErr: err,
		}
	}

	return

}
