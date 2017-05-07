package zfs

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
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
