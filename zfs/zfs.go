package zfs

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/zrepl/zrepl/util/circlog"
	"github.com/zrepl/zrepl/util/envconst"
)

var (
	ZFSSendPipeCapacityHint = int(envconst.Int64("ZFS_SEND_PIPE_CAPACITY_HINT", 1<<25))
	ZFSRecvPipeCapacityHint = int(envconst.Int64("ZFS_RECV_PIPE_CAPACITY_HINT", 1<<25))
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

func (p *DatasetPath) Pool() (string, error) {
	if len(p.comps) < 1 {
		return "", fmt.Errorf("dataset path does not have a pool component")
	}
	return p.comps[0], nil

}

func NewDatasetPath(s string) (p *DatasetPath, err error) {
	p = &DatasetPath{}
	if s == "" {
		p.comps = make([]string, 0)
		return p, nil // the empty dataset path
	}
	const FORBIDDEN = "@#|\t<>*"
	/* Documentation of allowed characters in zfs names:
	https://docs.oracle.com/cd/E19253-01/819-5461/gbcpt/index.html
	Space is missing in the oracle list, but according to
	https://github.com/zfsonlinux/zfs/issues/439
	there is evidence that it was intentionally allowed
	*/
	if strings.ContainsAny(s, FORBIDDEN) {
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

func (e *ZFSError) Error() string {
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
		err := &ZFSError{
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

	cmd := exec.CommandContext(ctx, ZFS_BINARY, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		sendResult(nil, err)
		return
	}
	// TODO bounded buffer
	stderr := bytes.NewBuffer(make([]byte, 0, 1024))
	cmd.Stderr = stderr
	if err = cmd.Start(); err != nil {
		sendResult(nil, err)
		return
	}
	defer func() {
		// discard the error, this defer is only relevant if we return while parsing the output
		// in which case we'll return an 'unexpected output' error and not the exit status
		_ = cmd.Wait()
	}()

	s := bufio.NewScanner(stdout)
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
	if err := cmd.Wait(); err != nil {
		if err, ok := err.(*exec.ExitError); ok {
			sendResult(nil, &ZFSError{
				Stderr:  stderr.Bytes(),
				WaitErr: err,
			})
		} else {
			sendResult(nil, &ZFSError{WaitErr: err})
		}
		return
	}
	if s.Err() != nil {
		sendResult(nil, s.Err())
		return
	}
}

// FIXME replace with EntityNamecheck
func validateZFSFilesystem(fs string) error {
	if len(fs) < 1 {
		return errors.New("filesystem path must have length > 0")
	}
	return nil
}

// v must not be nil and be already validated
func absVersion(fs string, v *ZFSSendArgVersion) (full string, err error) {
	if err := validateZFSFilesystem(fs); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s", fs, v.RelName), nil
}

// tok is allowed to be nil
// a must already be validated
//
// SECURITY SENSITIVE because Raw must be handled correctly
func (a ZFSSendArgsUnvalidated) buildCommonSendArgs() ([]string, error) {

	args := make([]string, 0, 3)
	// ResumeToken takes precedence, we assume that it has been validated to reflect
	// what is described by the other fields in ZFSSendArgs
	if a.ResumeToken != "" {
		args = append(args, "-t", a.ResumeToken)
		return args, nil
	}

	if a.Encrypted.B {
		args = append(args, "-w")
	}

	toV, err := absVersion(a.FS, a.To)
	if err != nil {
		return nil, err
	}

	fromV := ""
	if a.From != nil {
		fromV, err = absVersion(a.FS, a.From)
		if err != nil {
			return nil, err
		}
	}

	if fromV == "" { // Initial
		args = append(args, toV)
	} else {
		args = append(args, "-i", fromV, toV)
	}
	return args, nil
}

type ReadCloserCopier struct {
	recorder readErrRecorder
}

type readErrRecorder struct {
	io.ReadCloser
	readErr error
}

type sendStreamCopierError struct {
	isReadErr bool // if false, it's a write error
	err       error
}

func (e sendStreamCopierError) Error() string {
	if e.isReadErr {
		return fmt.Sprintf("stream: read error: %s", e.err)
	} else {
		return fmt.Sprintf("stream: writer error: %s", e.err)
	}
}

func (e sendStreamCopierError) IsReadError() bool  { return e.isReadErr }
func (e sendStreamCopierError) IsWriteError() bool { return !e.isReadErr }

func (r *readErrRecorder) Read(p []byte) (n int, err error) {
	n, err = r.ReadCloser.Read(p)
	r.readErr = err
	return n, err
}

func NewReadCloserCopier(stream io.ReadCloser) *ReadCloserCopier {
	return &ReadCloserCopier{recorder: readErrRecorder{stream, nil}}
}

func (c *ReadCloserCopier) WriteStreamTo(w io.Writer) StreamCopierError {
	debug("sendStreamCopier.WriteStreamTo: begin")
	_, err := io.Copy(w, &c.recorder)
	debug("sendStreamCopier.WriteStreamTo: copy done")
	if err != nil {
		if c.recorder.readErr != nil {
			return sendStreamCopierError{isReadErr: true, err: c.recorder.readErr}
		} else {
			return sendStreamCopierError{isReadErr: false, err: err}
		}
	}
	return nil
}

func (c *ReadCloserCopier) Read(p []byte) (n int, err error) {
	return c.recorder.Read(p)
}

func (c *ReadCloserCopier) Close() error {
	return c.recorder.ReadCloser.Close()
}

func pipeWithCapacityHint(capacity int) (r, w *os.File, err error) {
	if capacity <= 0 {
		panic(fmt.Sprintf("capacity must be positive %v", capacity))
	}
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	trySetPipeCapacity(stdoutWriter, capacity)
	return stdoutReader, stdoutWriter, nil
}

type sendStream struct {
	cmd  *exec.Cmd
	kill context.CancelFunc

	closeMtx     sync.Mutex
	stdoutReader *os.File
	stderrBuf    *circlog.CircularLog
	opErr        error
}

func (s *sendStream) Read(p []byte) (n int, err error) {
	s.closeMtx.Lock()
	opErr := s.opErr
	s.closeMtx.Unlock()
	if opErr != nil {
		return 0, opErr
	}

	n, err = s.stdoutReader.Read(p)
	if err != nil {
		debug("sendStream: read err: %T %s", err, err)
		// TODO we assume here that any read error is permanent
		// which is most likely the case for a local zfs send
		kwerr := s.killAndWait(err)
		debug("sendStream: killAndWait n=%v err= %T %s", n, kwerr, kwerr)
		// TODO we assume here that any read error is permanent
		return n, kwerr
	}
	return n, err
}

func (s *sendStream) Close() error {
	debug("sendStream: close called")
	return s.killAndWait(nil)
}

func (s *sendStream) killAndWait(precedingReadErr error) error {

	debug("sendStream: killAndWait enter")
	defer debug("sendStream: killAndWait leave")
	if precedingReadErr == io.EOF {
		// give the zfs process a little bit of time to terminate itself
		// if it holds this deadline, exitErr will be nil
		time.AfterFunc(200*time.Millisecond, s.kill)
	} else {
		s.kill()
	}

	// allow async kills from Close(), that's why we only take the mutex here
	s.closeMtx.Lock()
	defer s.closeMtx.Unlock()

	if s.opErr != nil {
		return s.opErr
	}

	waitErr := s.cmd.Wait()
	// distinguish between ExitError (which is actually a non-problem for us)
	// vs failed wait syscall (for which we give upper layers the chance to retyr)
	var exitErr *exec.ExitError
	if waitErr != nil {
		if ee, ok := waitErr.(*exec.ExitError); ok {
			exitErr = ee
		} else {
			return waitErr
		}
	}

	// now, after we know the program exited do we close the pipe
	var closePipeErr error
	if s.stdoutReader != nil {
		closePipeErr = s.stdoutReader.Close()
		if closePipeErr == nil {
			// avoid double-closes in case anything below doesn't work
			// and someone calls Close again
			s.stdoutReader = nil
		} else {
			return closePipeErr
		}
	}

	// we managed to tear things down, no let's give the user some pretty *ZFSError
	if exitErr != nil {
		s.opErr = &ZFSError{
			Stderr:  []byte(s.stderrBuf.String()),
			WaitErr: exitErr,
		}
	} else {
		s.opErr = fmt.Errorf("zfs send exited with status code 0")
	}

	// detect the edge where we're called from s.Read
	// after the pipe EOFed and zfs send exited without errors
	// this is actually the "hot" / nice path
	if exitErr == nil && precedingReadErr == io.EOF {
		return precedingReadErr
	}

	return s.opErr
}

// NOTE: When updating this struct, make sure to update funcs Validate ValidateCorrespondsToResumeToken
type ZFSSendArgVersion struct {
	RelName string
	GUID    uint64
}

func (v ZFSSendArgVersion) GetGuid() uint64                     { return v.GUID }
func (v ZFSSendArgVersion) ToSendArgVersion() ZFSSendArgVersion { return v }

func (v ZFSSendArgVersion) ValidateInMemory(fs string) error {
	if fs == "" {
		panic(fs)
	}
	if len(v.RelName) == 0 {
		return errors.New("`RelName` must not be empty")
	}

	var et EntityType
	switch v.RelName[0] {
	case '@':
		et = EntityTypeSnapshot
	case '#':
		et = EntityTypeBookmark
	default:
		return fmt.Errorf("`RelName` field must start with @ or #, got %q", v.RelName)
	}

	full := v.fullPathUnchecked(fs)
	if err := EntityNamecheck(full, et); err != nil {
		return err
	}

	return nil
}

func (v ZFSSendArgVersion) mustValidateInMemory(fs string) {
	if err := v.ValidateInMemory(fs); err != nil {
		panic(err)
	}
}

// fs must be not empty
func (a ZFSSendArgVersion) ValidateExistsAndGetVersion(ctx context.Context, fs string) (v FilesystemVersion, _ error) {

	if err := a.ValidateInMemory(fs); err != nil {
		return v, nil
	}

	realVersion, err := ZFSGetFilesystemVersion(a.FullPath(fs))
	if err != nil {
		return v, err
	}

	if realVersion.Guid != a.GUID {
		return v, fmt.Errorf("`GUID` field does not match real dataset's GUID: %q != %q", realVersion.Guid, a.GUID)
	}

	return realVersion, nil
}

func (a ZFSSendArgVersion) ValidateExists(ctx context.Context, fs string) error {
	_, err := a.ValidateExistsAndGetVersion(ctx, fs)
	return err
}

func (v ZFSSendArgVersion) FullPath(fs string) string {
	v.mustValidateInMemory(fs)
	return v.fullPathUnchecked(fs)
}

func (v ZFSSendArgVersion) fullPathUnchecked(fs string) string {
	return fmt.Sprintf("%s%s", fs, v.RelName)
}

func (v ZFSSendArgVersion) IsSnapshot() bool {
	v.mustValidateInMemory("unimportant")
	return v.RelName[0] == '@'
}

func (v ZFSSendArgVersion) MustBeBookmark() {
	v.mustValidateInMemory("unimportant")
	if v.RelName[0] != '#' {
		panic(fmt.Sprintf("must be bookmark, got %q", v.RelName))
	}
}

type NilBool struct{ B bool }

func (n *NilBool) Validate() error {
	if n == nil {
		return fmt.Errorf("must explicitly set `true` or `false`")
	}
	return nil
}

func (n *NilBool) String() string {
	if n == nil {
		return "unset"
	}
	return fmt.Sprintf("%v", n.B)
}

// When updating this struct, check Validate and ValidateCorrespondsToResumeToken (POTENTIALLY SECURITY SENSITIVE)
type ZFSSendArgsUnvalidated struct {
	FS        string
	From, To  *ZFSSendArgVersion // From may be nil
	Encrypted *NilBool

	// Preferred if not empty
	ResumeToken string // if not nil, must match what is specified in From, To (covered by ValidateCorrespondsToResumeToken)
}

type ZFSSendArgsValidated struct {
	ZFSSendArgsUnvalidated
	FromVersion *FilesystemVersion
	ToVersion   FilesystemVersion
}

type zfsSendArgsValidationContext struct {
	encEnabled *NilBool
}

type ZFSSendArgsValidationErrorCode int

const (
	ZFSSendArgsGenericValidationError ZFSSendArgsValidationErrorCode = 1 + iota
	ZFSSendArgsEncryptedSendRequestedButFSUnencrypted
	ZFSSendArgsFSEncryptionCheckFail
	ZFSSendArgsResumeTokenMismatch
)

type ZFSSendArgsValidationError struct {
	Args ZFSSendArgsUnvalidated
	What ZFSSendArgsValidationErrorCode
	Msg  error
}

func newValidationError(sendArgs ZFSSendArgsUnvalidated, what ZFSSendArgsValidationErrorCode, cause error) *ZFSSendArgsValidationError {
	return &ZFSSendArgsValidationError{sendArgs, what, cause}
}

func newGenericValidationError(sendArgs ZFSSendArgsUnvalidated, cause error) *ZFSSendArgsValidationError {
	return &ZFSSendArgsValidationError{sendArgs, ZFSSendArgsGenericValidationError, cause}
}

func (e ZFSSendArgsValidationError) Error() string {
	return e.Msg.Error()
}

// - Recursively call Validate on each field.
// - Make sure that if ResumeToken != "", it reflects the same operation as the other parameters would.
//
// This function is not pure because GUIDs are checked against the local host's datasets.
func (a ZFSSendArgsUnvalidated) Validate(ctx context.Context) (v ZFSSendArgsValidated, _ error) {
	if dp, err := NewDatasetPath(a.FS); err != nil || dp.Length() == 0 {
		return v, newGenericValidationError(a, fmt.Errorf("`FS` must be a valid non-zero dataset path"))
	}

	if a.To == nil {
		return v, newGenericValidationError(a, fmt.Errorf("`To` must not be nil"))
	}
	toVersion, err := a.To.ValidateExistsAndGetVersion(ctx, a.FS)
	if err != nil {
		return v, newGenericValidationError(a, errors.Wrap(err, "`To` invalid"))
	}

	var fromVersion *FilesystemVersion
	if a.From != nil {
		fromV, err := a.From.ValidateExistsAndGetVersion(ctx, a.FS)
		if err != nil {
			return v, newGenericValidationError(a, errors.Wrap(err, "`From` invalid"))
		}
		fromVersion = &fromV
		// fallthrough
	}

	if err := a.Encrypted.Validate(); err != nil {
		return v, newGenericValidationError(a, errors.Wrap(err, "`Raw` invalid"))
	}

	valCtx := &zfsSendArgsValidationContext{}
	fsEncrypted, err := ZFSGetEncryptionEnabled(ctx, a.FS)
	if err != nil {
		return v, newValidationError(a, ZFSSendArgsFSEncryptionCheckFail,
			errors.Wrapf(err, "cannot check whether filesystem %q is encrypted", a.FS))
	}
	valCtx.encEnabled = &NilBool{fsEncrypted}

	if a.Encrypted.B && !fsEncrypted {
		return v, newValidationError(a, ZFSSendArgsEncryptedSendRequestedButFSUnencrypted,
			errors.Errorf("encrypted send requested, but filesystem %q is not encrypted", a.FS))
	}

	if a.ResumeToken != "" {
		if err := a.validateCorrespondsToResumeToken(ctx, valCtx); err != nil {
			return v, newValidationError(a, ZFSSendArgsResumeTokenMismatch, err)
		}
	}

	return ZFSSendArgsValidated{
		ZFSSendArgsUnvalidated: a,
		FromVersion:            fromVersion,
		ToVersion:              toVersion,
	}, nil
}

type ZFSSendArgsResumeTokenMismatchError struct {
	What ZFSSendArgsResumeTokenMismatchErrorCode
	Err  error
}

func (e *ZFSSendArgsResumeTokenMismatchError) Error() string { return e.Err.Error() }

type ZFSSendArgsResumeTokenMismatchErrorCode int

// The format is ZFSSendArgsResumeTokenMismatch+WhatIsWrongInToken
const (
	ZFSSendArgsResumeTokenMismatchGeneric          ZFSSendArgsResumeTokenMismatchErrorCode = 1 + iota
	ZFSSendArgsResumeTokenMismatchEncryptionNotSet                                         // encryption not set in token but required by send args
	ZFSSendArgsResumeTokenMismatchEncryptionSet                                            // encryption not set in token but not required by send args
	ZFSSendArgsResumeTokenMismatchFilesystem
)

func (c ZFSSendArgsResumeTokenMismatchErrorCode) fmt(format string, args ...interface{}) *ZFSSendArgsResumeTokenMismatchError {
	return &ZFSSendArgsResumeTokenMismatchError{
		What: c,
		Err:  fmt.Errorf(format, args...),
	}
}

// This is SECURITY SENSITIVE and requires exhaustive checking of both side's values
// An attacker requesting a Send with a crafted ResumeToken may encode different parameters in the resume token than expected:
// for example, they may specify another file system (e.g. the filesystem with secret data) or request unencrypted send instead of encrypted raw send.
func (a ZFSSendArgsUnvalidated) validateCorrespondsToResumeToken(ctx context.Context, valCtx *zfsSendArgsValidationContext) error {

	if a.ResumeToken == "" {
		return nil // nothing to do
	}

	debug("decoding resume token %q", a.ResumeToken)
	t, err := ParseResumeToken(ctx, a.ResumeToken)
	debug("decode resume token result: %#v %T %v", t, err, err)
	if err != nil {
		return err
	}

	tokenFS, _, err := t.ToNameSplit()
	if err != nil {
		return err
	}

	gen := ZFSSendArgsResumeTokenMismatchGeneric

	if a.FS != tokenFS.ToString() {
		return ZFSSendArgsResumeTokenMismatchFilesystem.fmt(
			"filesystem in resume token field `toname` = %q does not match expected value %q", tokenFS.ToString(), a.FS)
	}

	if (a.From != nil) != t.HasFromGUID { // existence must be same
		if t.HasFromGUID {
			return gen.fmt("resume token not expected to be incremental, but `fromguid` = %q", t.FromGUID)
		} else {
			return gen.fmt("resume token expected to be incremental, but `fromguid` not present")
		}
	} else if t.HasFromGUID { // if exists (which is same, we checked above), they must match
		if t.FromGUID != a.From.GUID {
			return gen.fmt("resume token `fromguid` != expected: %q != %q", t.FromGUID, a.From.GUID)
		}
	} else {
		_ = struct{}{} // both empty, ok
	}

	// To must never be empty
	if !t.HasToGUID {
		return gen.fmt("resume token does not have `toguid`")
	}
	if t.ToGUID != a.To.GUID { // a.To != nil because Validate checks for that
		return gen.fmt("resume token `toguid` != expected: %q != %q", t.ToGUID, a.To.GUID)
	}

	if a.Encrypted.B {
		if !(t.RawOK && t.CompressOK) {
			return ZFSSendArgsResumeTokenMismatchEncryptionNotSet.fmt(
				"resume token must have `rawok` and `compressok` = true but got %v %v", t.RawOK, t.CompressOK)
		}
		// fallthrough
	} else {
		if t.RawOK || t.CompressOK {
			return ZFSSendArgsResumeTokenMismatchEncryptionSet.fmt(
				"resume token must not have `rawok` or `compressok` set but got %v %v", t.RawOK, t.CompressOK)
		}
		// fallthrough
	}

	return nil
}

var zfsSendStderrCaptureMaxSize = envconst.Int("ZREPL_ZFS_SEND_STDERR_MAX_CAPTURE_SIZE", 1<<15)

var ErrEncryptedSendNotSupported = fmt.Errorf("raw sends which are required for encrypted zfs send are not supported")

// if token != "", then send -t token is used
// otherwise send [-i from] to is used
// (if from is "" a full ZFS send is done)
//
// Returns ErrEncryptedSendNotSupported if encrypted send is requested but not supported by CLI
func ZFSSend(ctx context.Context, sendArgs ZFSSendArgsValidated) (*ReadCloserCopier, error) {

	args := make([]string, 0)
	args = append(args, "send")

	// pre-validation of sendArgs for plain ErrEncryptedSendNotSupported error
	// TODO go1.13: push this down to sendArgs.Validate
	if encryptedSendValid := sendArgs.Encrypted.Validate(); encryptedSendValid == nil && sendArgs.Encrypted.B {
		supported, err := EncryptionCLISupported(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "cannot determine CLI native encryption support")
		}
		if !supported {
			return nil, ErrEncryptedSendNotSupported
		}
	}

	sargs, err := sendArgs.buildCommonSendArgs()
	if err != nil {
		return nil, err
	}
	args = append(args, sargs...)

	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, ZFS_BINARY, args...)

	// setup stdout with an os.Pipe to control pipe buffer size
	stdoutReader, stdoutWriter, err := pipeWithCapacityHint(ZFSSendPipeCapacityHint)
	if err != nil {
		cancel()
		return nil, err
	}

	cmd.Stdout = stdoutWriter

	stderrBuf := circlog.MustNewCircularLog(zfsSendStderrCaptureMaxSize)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		cancel()
		stdoutWriter.Close()
		stdoutReader.Close()
		return nil, errors.Wrap(err, "cannot start zfs send command")
	}
	stdoutWriter.Close()

	stream := &sendStream{
		cmd:          cmd,
		kill:         cancel,
		stdoutReader: stdoutReader,
		stderrBuf:    stderrBuf,
	}

	return NewReadCloserCopier(stream), nil
}

type DrySendType string

const (
	DrySendTypeFull        DrySendType = "full"
	DrySendTypeIncremental DrySendType = "incremental"
)

func DrySendTypeFromString(s string) (DrySendType, error) {
	switch s {
	case string(DrySendTypeFull):
		return DrySendTypeFull, nil
	case string(DrySendTypeIncremental):
		return DrySendTypeIncremental, nil
	default:
		return "", fmt.Errorf("unknown dry send type %q", s)
	}
}

type DrySendInfo struct {
	Type         DrySendType
	Filesystem   string // parsed from To field
	From, To     string // direct copy from ZFS output
	SizeEstimate int64  // -1 if size estimate is not possible
}

var (
	// keep same number of capture groups for unmarshalInfoLine homogeneity

	sendDryRunInfoLineRegexFull = regexp.MustCompile(`^(full)\t()([^\t]+@[^\t]+)\t([0-9]+)$`)
	// cannot enforce '[#@]' in incremental source, see test cases
	sendDryRunInfoLineRegexIncremental = regexp.MustCompile(`^(incremental)\t([^\t]+)\t([^\t]+@[^\t]+)\t([0-9]+)$`)
)

// see test cases for example output
func (s *DrySendInfo) unmarshalZFSOutput(output []byte) (err error) {
	debug("DrySendInfo.unmarshalZFSOutput: output=%q", output)
	lines := strings.Split(string(output), "\n")
	for _, l := range lines {
		regexMatched, err := s.unmarshalInfoLine(l)
		if err != nil {
			return fmt.Errorf("line %q: %s", l, err)
		}
		if !regexMatched {
			continue
		}
		return nil
	}
	return fmt.Errorf("no match for info line (regex1 %s) (regex2 %s)", sendDryRunInfoLineRegexFull, sendDryRunInfoLineRegexIncremental)
}

// unmarshal info line, looks like this:
//   full	zroot/test/a@1	5389768
//   incremental	zroot/test/a@1	zroot/test/a@2	5383936
// => see test cases
func (s *DrySendInfo) unmarshalInfoLine(l string) (regexMatched bool, err error) {

	mFull := sendDryRunInfoLineRegexFull.FindStringSubmatch(l)
	mInc := sendDryRunInfoLineRegexIncremental.FindStringSubmatch(l)
	var m []string
	if mFull == nil && mInc == nil {
		return false, nil
	} else if mFull != nil && mInc != nil {
		panic(fmt.Sprintf("ambiguous ZFS dry send output: %q", l))
	} else if mFull != nil {
		m = mFull
	} else if mInc != nil {
		m = mInc
	}
	s.Type, err = DrySendTypeFromString(m[1])
	if err != nil {
		return true, err
	}

	s.From = m[2]
	s.To = m[3]
	toFS, _, _, err := DecomposeVersionString(s.To)
	if err != nil {
		return true, fmt.Errorf("'to' is not a valid filesystem version: %s", err)
	}
	s.Filesystem = toFS

	s.SizeEstimate, err = strconv.ParseInt(m[4], 10, 64)
	if err != nil {
		return true, fmt.Errorf("cannot not parse size: %s", err)
	}

	return true, nil
}

// to may be "", in which case a full ZFS send is done
// May return BookmarkSizeEstimationNotSupported as err if from is a bookmark.
func ZFSSendDry(ctx context.Context, sendArgs ZFSSendArgsValidated) (_ *DrySendInfo, err error) {

	if sendArgs.From != nil && strings.Contains(sendArgs.From.RelName, "#") {
		/* TODO:
		 * ZFS at the time of writing does not support dry-run send because size-estimation
		 * uses fromSnap's deadlist. However, for a bookmark, that deadlist no longer exists.
		 * Redacted send & recv will bring this functionality, see
		 * 	https://github.com/openzfs/openzfs/pull/484
		 */
		fromAbs, err := absVersion(sendArgs.FS, sendArgs.From)
		if err != nil {
			return nil, fmt.Errorf("error building abs version for 'from': %s", err)
		}
		toAbs, err := absVersion(sendArgs.FS, sendArgs.To)
		if err != nil {
			return nil, fmt.Errorf("error building abs version for 'to': %s", err)
		}
		return &DrySendInfo{
			Type:         DrySendTypeIncremental,
			Filesystem:   sendArgs.FS,
			From:         fromAbs,
			To:           toAbs,
			SizeEstimate: -1}, nil
	}

	args := make([]string, 0)
	args = append(args, "send", "-n", "-v", "-P")
	sargs, err := sendArgs.buildCommonSendArgs()
	if err != nil {
		return nil, err
	}
	args = append(args, sargs...)

	cmd := exec.Command(ZFS_BINARY, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, &ZFSError{output, err}
	}
	var si DrySendInfo
	if err := si.unmarshalZFSOutput(output); err != nil {
		return nil, fmt.Errorf("could not parse zfs send -n output: %s", err)
	}
	return &si, nil
}

type StreamCopierError interface {
	error
	IsReadError() bool
	IsWriteError() bool
}

type StreamCopier interface {
	// WriteStreamTo writes the stream represented by this StreamCopier
	// to the given io.Writer.
	WriteStreamTo(w io.Writer) StreamCopierError
	// Close must be called as soon as it is clear that no more data will
	// be read from the StreamCopier.
	// If StreamCopier gets its data from a connection, it might hold
	// a lock on the connection until Close is called. Only closing ensures
	// that the connection can be used afterwards.
	Close() error
}

type RecvOptions struct {
	// Rollback to the oldest snapshot, destroy it, then perform `recv -F`.
	// Note that this doesn't change property values, i.e. an existing local property value will be kept.
	RollbackAndForceRecv bool
	// Set -s flag used for resumable send & recv
	SavePartialRecvState bool
}

type ErrRecvResumeNotSupported struct {
	FS       string
	CheckErr error
}

func (e *ErrRecvResumeNotSupported) Error() string {
	var buf strings.Builder
	fmt.Fprintf(&buf, "zfs resumable recv into %q: ", e.FS)
	if e.CheckErr != nil {
		fmt.Fprint(&buf, e.CheckErr.Error())
	} else {
		fmt.Fprintf(&buf, "not supported by ZFS or pool")
	}
	return buf.String()
}

func ZFSRecv(ctx context.Context, fs string, v *ZFSSendArgVersion, streamCopier StreamCopier, opts RecvOptions) (err error) {

	if err := v.ValidateInMemory(fs); err != nil {
		return errors.Wrap(err, "invalid version")
	}
	if !v.IsSnapshot() {
		return errors.New("must receive into a snapshot")
	}

	fsdp, err := NewDatasetPath(fs)
	if err != nil {
		return err
	}

	if opts.RollbackAndForceRecv {
		// destroy all snapshots before `recv -F` because `recv -F`
		// does not perform a rollback unless `send -R` was used (which we assume hasn't been the case)
		var snaps []FilesystemVersion
		{
			vs, err := ZFSListFilesystemVersions(fsdp, nil)
			if err != nil {
				return fmt.Errorf("cannot list versions for rollback for forced receive: %s", err)
			}
			for _, v := range vs {
				if v.Type == Snapshot {
					snaps = append(snaps, v)
				}
			}
			sort.Slice(snaps, func(i, j int) bool {
				return snaps[i].CreateTXG < snaps[j].CreateTXG
			})
		}
		// bookmarks are rolled back automatically
		if len(snaps) > 0 {
			// use rollback to efficiently destroy all but the earliest snapshot
			// then destroy that earliest snapshot
			// afterwards, `recv -F` will work
			rollbackTarget := snaps[0]
			rollbackTargetAbs := rollbackTarget.ToAbsPath(fsdp)
			debug("recv: rollback to %q", rollbackTargetAbs)
			if err := ZFSRollback(fsdp, rollbackTarget, "-r"); err != nil {
				return fmt.Errorf("cannot rollback %s to %s for forced receive: %s", fsdp.ToString(), rollbackTarget, err)
			}
			debug("recv: destroy %q", rollbackTargetAbs)
			if err := ZFSDestroy(rollbackTargetAbs); err != nil {
				return fmt.Errorf("cannot destroy %s for forced receive: %s", rollbackTargetAbs, err)
			}
		}
	}

	args := make([]string, 0)
	args = append(args, "recv")
	if opts.RollbackAndForceRecv {
		args = append(args, "-F")
	}
	if opts.SavePartialRecvState {
		if supported, err := ResumeRecvSupported(ctx, fsdp); err != nil || !supported {
			return &ErrRecvResumeNotSupported{FS: fs, CheckErr: err}
		}
		args = append(args, "-s")
	}
	args = append(args, v.FullPath(fs))

	ctx, cancelCmd := context.WithCancel(ctx)
	defer cancelCmd()
	cmd := exec.CommandContext(ctx, ZFS_BINARY, args...)

	stderr := bytes.NewBuffer(make([]byte, 0, 1024))
	cmd.Stderr = stderr

	// TODO report bug upstream
	// Setup an unused stdout buffer.
	// Otherwise, ZoL v0.6.5.9-1 3.16.0-4-amd64 writes the following error to stderr and exits with code 1
	//   cannot receive new filesystem stream: invalid backup stream
	stdout := bytes.NewBuffer(make([]byte, 0, 1024))
	cmd.Stdout = stdout

	stdin, stdinWriter, err := pipeWithCapacityHint(ZFSRecvPipeCapacityHint)
	if err != nil {
		return err
	}

	cmd.Stdin = stdin

	if err = cmd.Start(); err != nil {
		stdinWriter.Close()
		stdin.Close()
		return err
	}
	stdin.Close()
	defer stdinWriter.Close()

	pid := cmd.Process.Pid
	debug := func(format string, args ...interface{}) {
		debug("recv: pid=%v: %s", pid, fmt.Sprintf(format, args...))
	}

	debug("started")

	copierErrChan := make(chan StreamCopierError)
	go func() {
		copierErrChan <- streamCopier.WriteStreamTo(stdinWriter)
		stdinWriter.Close()
	}()
	waitErrChan := make(chan error)
	go func() {
		defer close(waitErrChan)
		if err = cmd.Wait(); err != nil {
			if rtErr := tryRecvErrorWithResumeToken(ctx, stderr.String()); rtErr != nil {
				waitErrChan <- rtErr
			} else {
				waitErrChan <- &ZFSError{
					Stderr:  stderr.Bytes(),
					WaitErr: err,
				}
			}
			return
		}
	}()

	// streamCopier always fails before or simultaneously with Wait
	// thus receive from it first
	copierErr := <-copierErrChan
	debug("copierErr: %T %s", copierErr, copierErr)
	if copierErr != nil {
		cancelCmd()
	}

	waitErr := <-waitErrChan
	debug("waitErr: %T %s", waitErr, waitErr)
	if copierErr == nil && waitErr == nil {
		return nil
	} else if waitErr != nil && (copierErr == nil || copierErr.IsWriteError()) {
		return waitErr // has more interesting info in that case
	}
	return copierErr // if it's not a write error, the copier error is more interesting
}

type RecvFailedWithResumeTokenErr struct {
	Msg               string
	ResumeTokenRaw    string
	ResumeTokenParsed *ResumeToken
}

var recvErrorResumeTokenRE = regexp.MustCompile(`A resuming stream can be generated on the sending system by running:\s+zfs send -t\s(\S+)`)

func tryRecvErrorWithResumeToken(ctx context.Context, stderr string) *RecvFailedWithResumeTokenErr {
	if match := recvErrorResumeTokenRE.FindStringSubmatch(stderr); match != nil {
		parsed, err := ParseResumeToken(ctx, match[1])
		if err != nil {
			return nil
		}
		return &RecvFailedWithResumeTokenErr{
			Msg:               stderr,
			ResumeTokenRaw:    match[1],
			ResumeTokenParsed: parsed,
		}
	}
	return nil
}

func (e *RecvFailedWithResumeTokenErr) Error() string {
	return fmt.Sprintf("receive failed, resume token available: %s\n%#v", e.ResumeTokenRaw, e.ResumeTokenParsed)
}

type ClearResumeTokenError struct {
	ZFSOutput []byte
	CmdError  error
}

func (e ClearResumeTokenError) Error() string {
	return fmt.Sprintf("could not clear resume token: %q", string(e.ZFSOutput))
}

// always returns *ClearResumeTokenError
func ZFSRecvClearResumeToken(fs string) (err error) {
	if err := validateZFSFilesystem(fs); err != nil {
		return err
	}

	cmd := exec.Command(ZFS_BINARY, "recv", "-A", fs)
	o, err := cmd.CombinedOutput()
	if err != nil {
		if bytes.Contains(o, []byte("does not have any resumable receive state to abort")) {
			return nil
		}
		return &ClearResumeTokenError{o, err}
	}
	return nil
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
		err = &ZFSError{
			Stderr:  stderr.Bytes(),
			WaitErr: err,
		}
	}

	return
}

func ZFSGet(fs *DatasetPath, props []string) (*ZFSProperties, error) {
	return zfsGet(fs.ToString(), props, sourceAny)
}

// The returned error includes requested filesystem and version as quoted strings in its error message
func ZFSGetGUID(fs string, version string) (g uint64, err error) {
	defer func(e *error) {
		if *e != nil {
			*e = fmt.Errorf("zfs get guid fs=%q version=%q: %s", fs, version, *e)
		}
	}(&err)
	if err := validateZFSFilesystem(fs); err != nil {
		return 0, err
	}
	if len(version) == 0 {
		return 0, errors.New("version must have non-zero length")
	}
	if strings.IndexAny(version[0:1], "@#") != 0 {
		return 0, errors.New("version does not start with @ or #")
	}
	path := fmt.Sprintf("%s%s", fs, version)
	props, err := zfsGet(path, []string{"guid"}, sourceAny) // always local
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(props.Get("guid"), 10, 64)
}

type GetMountpointOutput struct {
	Mounted    bool
	Mountpoint string
}

func ZFSGetMountpoint(fs string) (*GetMountpointOutput, error) {
	if err := EntityNamecheck(fs, EntityTypeFilesystem); err != nil {
		return nil, err
	}
	props, err := zfsGet(fs, []string{"mountpoint", "mounted"}, sourceAny)
	if err != nil {
		return nil, err
	}
	o := &GetMountpointOutput{}
	o.Mounted = props.Get("mounted") == "yes"
	o.Mountpoint = props.Get("mountpoint")
	if o.Mountpoint == "none" {
		o.Mountpoint = ""
	}
	if o.Mounted && o.Mountpoint == "" {
		panic("unexpected zfs get output")
	}
	return o, nil
}

func ZFSGetRawAnySource(path string, props []string) (*ZFSProperties, error) {
	return zfsGet(path, props, sourceAny)
}

var zfsGetDatasetDoesNotExistRegexp = regexp.MustCompile(`^cannot open '([^)]+)': (dataset does not exist|no such pool or dataset)`) // verified in platformtest

type DatasetDoesNotExist struct {
	Path string
}

func (d *DatasetDoesNotExist) Error() string { return fmt.Sprintf("dataset %q does not exist", d.Path) }

func tryDatasetDoesNotExist(expectPath string, stderr []byte) error {
	if sm := zfsGetDatasetDoesNotExistRegexp.FindSubmatch(stderr); sm != nil {
		if string(sm[1]) == expectPath {
			return &DatasetDoesNotExist{expectPath}
		}
	}
	return nil
}

type zfsPropertySource uint

const (
	sourceLocal zfsPropertySource = 1 << iota
	sourceDefault
	sourceInherited
	sourceNone
	sourceTemporary
	sourceReceived

	sourceAny zfsPropertySource = ^zfsPropertySource(0)
)

func (s zfsPropertySource) zfsGetSourceFieldPrefixes() []string {
	prefixes := make([]string, 0, 7)
	if s&sourceLocal != 0 {
		prefixes = append(prefixes, "local")
	}
	if s&sourceDefault != 0 {
		prefixes = append(prefixes, "default")
	}
	if s&sourceInherited != 0 {
		prefixes = append(prefixes, "inherited")
	}
	if s&sourceNone != 0 {
		prefixes = append(prefixes, "-")
	}
	if s&sourceTemporary != 0 {
		prefixes = append(prefixes, "temporary")
	}
	if s&sourceReceived != 0 {
		prefixes = append(prefixes, "received")
	}
	if s == sourceAny {
		prefixes = append(prefixes, "")
	}
	return prefixes
}

func zfsGet(path string, props []string, allowedSources zfsPropertySource) (*ZFSProperties, error) {
	args := []string{"get", "-Hp", "-o", "property,value,source", strings.Join(props, ","), path}
	cmd := exec.Command(ZFS_BINARY, args...)
	stdout, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.Exited() {
				// screen-scrape output
				if ddne := tryDatasetDoesNotExist(path, exitErr.Stderr); ddne != nil {
					return nil, ddne
				}
			}
			return nil, &ZFSError{
				Stderr:  exitErr.Stderr,
				WaitErr: exitErr,
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
	allowedPrefixes := allowedSources.zfsGetSourceFieldPrefixes()
	for _, line := range lines[:len(lines)-1] {
		fields := strings.FieldsFunc(line, func(r rune) bool {
			return r == '\t'
		})
		if len(fields) != 3 {
			return nil, fmt.Errorf("zfs get did not return property,value,source tuples")
		}
		for _, p := range allowedPrefixes {
			if strings.HasPrefix(fields[2], p) {
				res.m[fields[0]] = fields[1]
				break
			}
		}
	}
	return res, nil
}

type DestroySnapshotsError struct {
	RawLines      []string
	Filesystem    string
	Undestroyable []string // snapshot name only (filesystem@ stripped)
	Reason        []string
}

func (e *DestroySnapshotsError) Error() string {
	if len(e.Undestroyable) != len(e.Reason) {
		panic(fmt.Sprintf("%v != %v", len(e.Undestroyable), len(e.Reason)))
	}
	if len(e.Undestroyable) == 0 {
		panic(fmt.Sprintf("error must have one undestroyable snapshot, %q", e.Filesystem))
	}
	if len(e.Undestroyable) == 1 {
		return fmt.Sprintf("zfs destroy failed: %s@%s: %s", e.Filesystem, e.Undestroyable[0], e.Reason[0])
	}
	return strings.Join(e.RawLines, "\n")
}

var destroySnapshotsErrorRegexp = regexp.MustCompile(`^cannot destroy snapshot ([^@]+)@(.+): (.*)$`) // yes, datasets can contain `:`

var destroyOneOrMoreSnapshotsNoneExistedErrorRegexp = regexp.MustCompile(`^could not find any snapshots to destroy; check snapshot names.`)

var destroyBookmarkDoesNotExist = regexp.MustCompile(`^bookmark '([^']+)' does not exist`)

func tryParseDestroySnapshotsError(arg string, stderr []byte) *DestroySnapshotsError {

	argComps := strings.SplitN(arg, "@", 2)
	if len(argComps) != 2 {
		return nil
	}
	filesystem := argComps[0]

	lines := bufio.NewScanner(bytes.NewReader(stderr))
	undestroyable := []string{}
	reason := []string{}
	rawLines := []string{}
	for lines.Scan() {
		line := lines.Text()
		rawLines = append(rawLines, line)
		m := destroySnapshotsErrorRegexp.FindStringSubmatch(line)
		if m == nil {
			return nil // unexpected line => be conservative
		} else {
			if m[1] != filesystem {
				return nil // unexpected line => be conservative
			}
			undestroyable = append(undestroyable, m[2])
			reason = append(reason, m[3])
		}
	}
	if len(undestroyable) == 0 {
		return nil
	}

	return &DestroySnapshotsError{
		RawLines:      rawLines,
		Filesystem:    filesystem,
		Undestroyable: undestroyable,
		Reason:        reason,
	}
}

func ZFSDestroy(arg string) (err error) {

	var dstype, filesystem string
	idx := strings.IndexAny(arg, "@#")
	if idx == -1 {
		dstype = "filesystem"
		filesystem = arg
	} else {
		switch arg[idx] {
		case '@':
			dstype = "snapshot"
		case '#':
			dstype = "bookmark"
		}
		filesystem = arg[:idx]
	}

	defer prometheus.NewTimer(prom.ZFSDestroyDuration.WithLabelValues(dstype, filesystem))

	cmd := exec.Command(ZFS_BINARY, "destroy", arg)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err = cmd.Start(); err != nil {
		return err
	}

	if err = cmd.Wait(); err != nil {
		err = &ZFSError{
			Stderr:  stderr.Bytes(),
			WaitErr: err,
		}

		if destroyOneOrMoreSnapshotsNoneExistedErrorRegexp.Match(stderr.Bytes()) {
			err = &DatasetDoesNotExist{arg}
		} else if match := destroyBookmarkDoesNotExist.FindStringSubmatch(stderr.String()); match != nil && match[1] == arg {
			err = &DatasetDoesNotExist{arg}
		} else if dsNotExistErr := tryDatasetDoesNotExist(filesystem, stderr.Bytes()); dsNotExistErr != nil {
			err = dsNotExistErr
		} else if dserr := tryParseDestroySnapshotsError(arg, stderr.Bytes()); dserr != nil {
			err = dserr
		}

	}

	return

}

func ZFSDestroyIdempotent(path string) error {
	err := ZFSDestroy(path)
	if _, ok := err.(*DatasetDoesNotExist); ok {
		return nil
	}
	return err
}

func ZFSSnapshot(fs *DatasetPath, name string, recursive bool) (err error) {

	promTimer := prometheus.NewTimer(prom.ZFSSnapshotDuration.WithLabelValues(fs.ToString()))
	defer promTimer.ObserveDuration()

	snapname := fmt.Sprintf("%s@%s", fs.ToString(), name)
	if err := EntityNamecheck(snapname, EntityTypeSnapshot); err != nil {
		return errors.Wrap(err, "zfs snapshot")
	}
	cmd := exec.Command(ZFS_BINARY, "snapshot", snapname)

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

var zfsBookmarkExistsRegex = regexp.MustCompile("^cannot create bookmark '[^']+': bookmark exists")

type BookmarkExists struct {
	zfsMsg         string
	fs, bookmark   string
	bookmarkOrigin ZFSSendArgVersion
	bookGuid       uint64
}

func (e *BookmarkExists) Error() string {
	return fmt.Sprintf("bookmark %s (guid=%v) with #%s: bookmark #%s exists but has different guid (%v)",
		e.bookmarkOrigin.FullPath(e.fs), e.bookmarkOrigin.GUID, e.bookmark, e.bookmark, e.bookGuid,
	)
}

var ErrBookmarkCloningNotSupported = fmt.Errorf("bookmark cloning feature is not yet supported by ZFS")

// idempotently create bookmark of the given version v
//
// v must be validated by the caller
//
// does not destroy an existing bookmark, returns
//
func ZFSBookmark(fs string, v ZFSSendArgVersion, bookmark string) (err error) {

	promTimer := prometheus.NewTimer(prom.ZFSBookmarkDuration.WithLabelValues(fs))
	defer promTimer.ObserveDuration()

	if !v.IsSnapshot() {
		return ErrBookmarkCloningNotSupported // TODO This is work in progress: https://github.com/zfsonlinux/zfs/pull/9571
	}

	snapname := v.FullPath(fs)
	if err := EntityNamecheck(snapname, EntityTypeSnapshot); err != nil {
		return err
	}
	bookmarkname := fmt.Sprintf("%s#%s", fs, bookmark)
	if err := EntityNamecheck(bookmarkname, EntityTypeBookmark); err != nil {
		return err
	}

	debug("bookmark: %q %q", snapname, bookmarkname)

	cmd := exec.Command(ZFS_BINARY, "bookmark", snapname, bookmarkname)

	stderr := bytes.NewBuffer(make([]byte, 0, 1024))
	cmd.Stderr = stderr

	if err = cmd.Start(); err != nil {
		return err
	}

	if err = cmd.Wait(); err != nil {

		if ddne := tryDatasetDoesNotExist(snapname, stderr.Bytes()); err != nil {
			return ddne
		} else if zfsBookmarkExistsRegex.Match(stderr.Bytes()) {

			// check if this was idempotent
			bookGuid, err := ZFSGetGUID(fs, "#"+bookmark)
			if err != nil {
				return errors.Wrap(err, "bookmark idempotency check") // guid error expressive enough
			}

			if v.GUID == bookGuid {
				debug("bookmark: %q %q was idempotent: {snap,book}guid %d == %d", snapname, bookmarkname, v.GUID, bookGuid)
				return nil
			}
			return &BookmarkExists{
				fs: fs, bookmarkOrigin: v, bookmark: bookmark,
				zfsMsg:   stderr.String(),
				bookGuid: bookGuid,
			}

		} else {
			return &ZFSError{
				Stderr:  stderr.Bytes(),
				WaitErr: err,
			}
		}

	}

	return nil

}

func ZFSRollback(fs *DatasetPath, snapshot FilesystemVersion, rollbackArgs ...string) (err error) {

	snapabs := snapshot.ToAbsPath(fs)
	if snapshot.Type != Snapshot {
		return fmt.Errorf("can only rollback to snapshots, got %s", snapabs)
	}

	args := []string{"rollback"}
	args = append(args, rollbackArgs...)
	args = append(args, snapabs)

	cmd := exec.Command(ZFS_BINARY, args...)

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

	return err
}
