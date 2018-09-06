package zfs

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"context"
	"github.com/problame/go-rwccmd"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/zrepl/zrepl/util"
	"regexp"
	"strconv"
)

type DatasetPath struct {
	comps []string
}

func (p *DatasetPath) ToString() string {
	return strings.Join(p.comps, "/")
}

func (p *DatasetPath) Empty() bool {
	return len(p.comps) == 0
}

func (p *DatasetPath) Extend(extend *DatasetPath) {
	p.comps = append(p.comps, extend.comps...)
}

func (p *DatasetPath) HasPrefix(prefix *DatasetPath) bool {
	if len(prefix.comps) > len(p.comps) {
		return false
	}
	for i := range prefix.comps {
		if prefix.comps[i] != p.comps[i] {
			return false
		}
	}
	return true
}

func (p *DatasetPath) TrimPrefix(prefix *DatasetPath) {
	if !p.HasPrefix(prefix) {
		return
	}
	prelen := len(prefix.comps)
	newlen := len(p.comps) - prelen
	oldcomps := p.comps
	p.comps = make([]string, newlen)
	for i := 0; i < newlen; i++ {
		p.comps[i] = oldcomps[prelen+i]
	}
	return
}

func (p *DatasetPath) TrimNPrefixComps(n int) {
	if len(p.comps) < n {
		n = len(p.comps)
	}
	if n == 0 {
		return
	}
	p.comps = p.comps[n:]

}

func (p DatasetPath) Equal(q *DatasetPath) bool {
	if len(p.comps) != len(q.comps) {
		return false
	}
	for i := range p.comps {
		if p.comps[i] != q.comps[i] {
			return false
		}
	}
	return true
}

func (p *DatasetPath) Length() int {
	return len(p.comps)
}

func (p *DatasetPath) Copy() (c *DatasetPath) {
	c = &DatasetPath{}
	c.comps = make([]string, len(p.comps))
	copy(c.comps, p.comps)
	return
}

func (p *DatasetPath) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.comps)
}

func (p *DatasetPath) UnmarshalJSON(b []byte) error {
	p.comps = make([]string, 0)
	return json.Unmarshal(b, &p.comps)
}

func NewDatasetPath(s string) (p *DatasetPath, err error) {
	p = &DatasetPath{}
	if s == "" {
		p.comps = make([]string, 0)
		return p, nil // the empty dataset path
	}
	const FORBIDDEN = "@#|\t <>*"
	if strings.ContainsAny(s, FORBIDDEN) { // TODO space may be a bit too restrictive...
		err = fmt.Errorf("contains forbidden characters (any of '%s')", FORBIDDEN)
		return
	}
	p.comps = strings.Split(s, "/")
	if p.comps[len(p.comps)-1] == "" {
		err = fmt.Errorf("must not end with a '/'")
		return
	}
	return
}

func toDatasetPath(s string) *DatasetPath {
	p, err := NewDatasetPath(s)
	if err != nil {
		panic(err)
	}
	return p
}

type ZFSError struct {
	Stderr  []byte
	WaitErr error
}

func (e ZFSError) Error() string {
	return fmt.Sprintf("zfs exited with error: %s\nstderr:\n%s", e.WaitErr.Error(), e.Stderr)
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

type ZFSListResult struct {
	Fields []string
	Err    error
}

// ZFSListChan executes `zfs list` and sends the results to the `out` channel.
// The `out` channel is always closed by ZFSListChan:
// If an error occurs, it is closed after sending a result with the Err field set.
// If no error occurs, it is just closed.
// If the operation is cancelled via context, the channel is just closed.
//
// However, if callers do not drain `out` or cancel via `ctx`, the process will leak either running because
// IO is pending or as a zombie.
func ZFSListChan(ctx context.Context, out chan ZFSListResult, properties []string, zfsArgs ...string) {
	defer close(out)

	args := make([]string, 0, 4+len(zfsArgs))
	args = append(args,
		"list", "-H", "-p",
		"-o", strings.Join(properties, ","))
	args = append(args, zfsArgs...)

	sendResult := func(fields []string, err error) (done bool) {
		select {
		case <-ctx.Done():
			return true
		case out <- ZFSListResult{fields, err}:
			return false
		}
	}

	cmd, err := rwccmd.CommandContext(ctx, ZFS_BINARY, args, []string{})
	if err != nil {
		sendResult(nil, err)
		return
	}
	if err = cmd.Start(); err != nil {
		sendResult(nil, err)
		return
	}
	defer cmd.Close()

	s := bufio.NewScanner(cmd)
	buf := make([]byte, 1024) // max line length
	s.Buffer(buf, 0)

	for s.Scan() {
		fields := strings.SplitN(s.Text(), "\t", len(properties))
		if len(fields) != len(properties) {
			sendResult(nil, errors.New("unexpected output"))
			return
		}
		if sendResult(fields, nil) {
			return
		}
	}
	if s.Err() != nil {
		sendResult(nil, s.Err())
	}
	return
}

func validateRelativeZFSVersion(s string) error {
	if len(s) <= 1 {
		return errors.New("version must start with a delimiter char followed by at least one character")
	}
	if !(s[0] == '#' || s[0] == '@') {
		return errors.New("version name starts with invalid delimiter char")
	}
	// FIXME whitespace check...
	return nil
}

func validateZFSFilesystem(fs string) error {
	if len(fs) < 1 {
		return errors.New("filesystem path must have length > 0")
	}
	return nil
}

func absVersion(fs, v string) (full string, err error) {
	if err := validateZFSFilesystem(fs); err != nil {
		return "", err
	}
	if err := validateRelativeZFSVersion(v); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s", fs, v), nil
}

func ZFSSend(fs string, from, to string) (stream io.ReadCloser, err error) {

	fromV, err := absVersion(fs, from)
	if err != nil {
		return nil, err
	}

	toV := ""
	if to != "" {
		toV, err = absVersion(fs, to)
		if err != nil {
			return nil, err
		}
	}

	args := make([]string, 0)
	args = append(args, "send")

	if toV == "" { // Initial
		args = append(args, fromV)
	} else {
		args = append(args, "-i", fromV, toV)
	}

	stream, err = util.RunIOCommand(ZFS_BINARY, args...)

	return
}

var BookmarkSizeEstimationNotSupported error = fmt.Errorf("size estimation is not supported for bookmarks")

// May return BookmarkSizeEstimationNotSupported as err if from is a bookmark.
func ZFSSendDry(fs string, from, to string) (size int64, err error) {

	fromV, err := absVersion(fs, from)
	if err != nil {
		return 0, err
	}

	toV := ""
	if to != "" {
		toV, err = absVersion(fs, to)
		if err != nil {
			return 0, err
		}
	}

	if strings.Contains(fromV, "#") {
		/* TODO:
		 * ZFS at the time of writing does not support dry-run send because size-estimation
		 * uses fromSnap's deadlist. However, for a bookmark, that deadlist no longer exists.
		 * Redacted send & recv will bring this functionality, see
		 * 	https://github.com/openzfs/openzfs/pull/484
		 */
		 return 0, BookmarkSizeEstimationNotSupported
	}

	args := make([]string, 0)
	args = append(args, "send", "-n", "-v", "-P")

	if toV == "" { // Initial
		args = append(args, fromV)
	} else {
		args = append(args, "-i", fromV, toV)
	}

	cmd := exec.Command(ZFS_BINARY, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, err
	}
	o := string(output)
	lines := strings.Split(o, "\n")
	if len(lines) < 2 {
		return 0, errors.New("zfs send -n did not return the expected number of lines")
	}
	fields := strings.Fields(lines[1])
	if len(fields) != 2 {
		return 0, errors.New("zfs send -n returned unexpexted output")
	}

	size, err = strconv.ParseInt(fields[1], 10, 64)
	return size, err
}

func ZFSRecv(fs string, stream io.Reader, additionalArgs ...string) (err error) {

	if err := validateZFSFilesystem(fs); err != nil {
		return err
	}

	args := make([]string, 0)
	args = append(args, "recv")
	if len(args) > 0 {
		args = append(args, additionalArgs...)
	}
	args = append(args, fs)

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

func ZFSRecvWriter(fs *DatasetPath, additionalArgs ...string) (io.WriteCloser, error) {

	args := make([]string, 0)
	args = append(args, "recv")
	if len(args) > 0 {
		args = append(args, additionalArgs...)
	}
	args = append(args, fs.ToString())

	cmd, err := util.NewIOCommand(ZFS_BINARY, args, 1024)
	if err != nil {
		return nil, err
	}

	if err = cmd.Start(); err != nil {
		return nil, err
	}

	return cmd.Stdin, nil
}

type ZFSProperties struct {
	m map[string]string
}

func NewZFSProperties() *ZFSProperties {
	return &ZFSProperties{make(map[string]string, 4)}
}

func (p *ZFSProperties) Set(key, val string) {
	p.m[key] = val
}

func (p *ZFSProperties) Get(key string) string {
	return p.m[key]
}

func (p *ZFSProperties) appendArgs(args *[]string) (err error) {
	for prop, val := range p.m {
		if strings.Contains(prop, "=") {
			return errors.New("prop contains rune '=' which is the delimiter between property name and value")
		}
		*args = append(*args, fmt.Sprintf("%s=%s", prop, val))
	}
	return nil
}

func ZFSSet(fs *DatasetPath, props *ZFSProperties) (err error) {
	return zfsSet(fs.ToString(), props)
}

func zfsSet(path string, props *ZFSProperties) (err error) {
	args := make([]string, 0)
	args = append(args, "set")
	err = props.appendArgs(&args)
	if err != nil {
		return err
	}
	args = append(args, path)

	cmd := exec.Command(ZFS_BINARY, args...)

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

func ZFSGet(fs *DatasetPath, props []string) (*ZFSProperties, error) {
	return zfsGet(fs.ToString(), props)
}

var zfsGetDatasetDoesNotExistRegexp = regexp.MustCompile(`^cannot open '(\S+)': (dataset does not exist|no such pool or dataset)`)

type DatasetDoesNotExist struct {
	Path string
}

func (d *DatasetDoesNotExist) Error() string { return fmt.Sprintf("dataset %q does not exist", d.Path) }

func zfsGet(path string, props []string) (*ZFSProperties, error) {
	args := []string{"get", "-Hp", "-o", "property,value", strings.Join(props, ","), path}
	cmd := exec.Command(ZFS_BINARY, args...)
	stdout, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.Exited() {
				// screen-scrape output
				if sm := zfsGetDatasetDoesNotExistRegexp.FindSubmatch(exitErr.Stderr); sm != nil {
					if string(sm[1]) == path {
						return nil, &DatasetDoesNotExist{path}
					}
				}
			}
		}
		return nil, err
	}
	o := string(stdout)
	lines := strings.Split(o, "\n")
	if len(lines) < 1 || // account for newlines
		len(lines)-1 != len(props) {
		return nil, fmt.Errorf("zfs get did not return the number of expected property values")
	}
	res := &ZFSProperties{
		make(map[string]string, len(lines)),
	}
	for _, line := range lines[:len(lines)-1] {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("zfs get did not return property value pairs")
		}
		res.m[fields[0]] = fields[1]
	}
	return res, nil
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

func zfsBuildSnapName(fs *DatasetPath, name string) string { // TODO defensive
	return fmt.Sprintf("%s@%s", fs.ToString(), name)
}

func zfsBuildBookmarkName(fs *DatasetPath, name string) string { // TODO defensive
	return fmt.Sprintf("%s#%s", fs.ToString(), name)
}

func ZFSSnapshot(fs *DatasetPath, name string, recursive bool) (err error) {

	promTimer := prometheus.NewTimer(prom.ZFSSnapshotDuration.WithLabelValues(fs.ToString()))
	defer promTimer.ObserveDuration()

	snapname := zfsBuildSnapName(fs, name)
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

func ZFSBookmark(fs *DatasetPath, snapshot, bookmark string) (err error) {

	promTimer := prometheus.NewTimer(prom.ZFSBookmarkDuration.WithLabelValues(fs.ToString()))
	defer promTimer.ObserveDuration()

	snapname := zfsBuildSnapName(fs, snapshot)
	bookmarkname := zfsBuildBookmarkName(fs, bookmark)

	cmd := exec.Command(ZFS_BINARY, "bookmark", snapname, bookmarkname)

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
