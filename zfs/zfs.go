package zfs

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/zrepl/zrepl/util/circlog"
	"github.com/zrepl/zrepl/util/envconst"
	"github.com/zrepl/zrepl/util/nodefault"
	zfsprop "github.com/zrepl/zrepl/zfs/property"
	"github.com/zrepl/zrepl/zfs/zfscmd"
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

func ZFSList(ctx context.Context, properties []string, zfsArgs ...string) (res [][]string, err error) {

	args := make([]string, 0, 4+len(zfsArgs))
	args = append(args,
		"list", "-H", "-p",
		"-o", strings.Join(properties, ","))
	args = append(args, zfsArgs...)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := zfscmd.CommandContext(ctx, ZFS_BINARY, args...)
	stdout, stderrBuf, err := cmd.StdoutPipeWithErrorBuf()
	if err != nil {
		return
	}

	if err = cmd.Start(); err != nil {
		return
	}
	// in case we return early, we want to kill the zfs list process and wait for it to exit
	defer func() {
		_ = cmd.Wait()
	}()
	defer cancel()

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
			Stderr:  stderrBuf.Bytes(),
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
// If notExistHint is not nil and zfs exits with an error,
// the stderr is attempted to be interpreted as a *DatasetDoesNotExist error.
//
// However, if callers do not drain `out` or cancel via `ctx`, the process will leak either running because
// IO is pending or as a zombie.
func ZFSListChan(ctx context.Context, out chan ZFSListResult, properties []string, notExistHint *DatasetPath, zfsArgs ...string) {
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

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := zfscmd.CommandContext(ctx, ZFS_BINARY, args...)
	stdout, stderrBuf, err := cmd.StdoutPipeWithErrorBuf()
	if err != nil {
		sendResult(nil, err)
		return
	}
	if err = cmd.Start(); err != nil {
		sendResult(nil, err)
		return
	}
	defer func() {
		// discard the error, this defer is only relevant if we return while parsing the output
		// in which case we'll return an 'unexpected output' error and not the exit status
		_ = cmd.Wait()
	}()
	defer cancel() // in case we return before our regular call to cmd.Wait(), kill the zfs list process

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
		if _, ok := err.(*exec.ExitError); ok {
			var enotexist *DatasetDoesNotExist
			if notExistHint != nil {
				enotexist = tryDatasetDoesNotExist(notExistHint.ToString(), stderrBuf.Bytes())
			}
			if enotexist != nil {
				sendResult(nil, enotexist)
			} else {
				sendResult(nil, &ZFSError{
					Stderr:  stderrBuf.Bytes(),
					WaitErr: err,
				})
			}
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

func pipeWithCapacityHint(capacity int) (r, w *os.File, err error) {
	if capacity < 0 {
		panic(fmt.Sprintf("capacity must be non-negative, got %v", capacity))
	}
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	trySetPipeCapacity(stdoutWriter, capacity)
	return stdoutReader, stdoutWriter, nil
}

type sendStreamState int

const (
	sendStreamOpen sendStreamState = iota
	sendStreamClosed
)

type SendStream struct {
	cmd          *zfscmd.Cmd
	kill         context.CancelFunc
	stdoutReader io.ReadCloser // not *os.File for mocking during platformtest
	stderrBuf    *circlog.CircularLog

	mtx     sync.Mutex
	state   sendStreamState
	exitErr *ZFSError
}

func (s *SendStream) Read(p []byte) (n int, _ error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	switch s.state {
	case sendStreamClosed:
		return 0, os.ErrClosed

	case sendStreamOpen:
		n, readErr := s.stdoutReader.Read(p)
		if readErr != nil {
			debug("sendStream: read: readErr=%T %s", readErr, readErr)
			if readErr == io.EOF {
				// io.EOF must be bubbled up as is so that consumers can handle it properly.
				return n, readErr
			}
			// Assume that the error is not retryable.
			// Try to kill now so that we can return a nice *ZFSError with captured stderr.
			// If the kill doesn't work, it doesn't matter because the caller must by contract call Close() anyways.
			killErr := s.killAndWait()
			debug("sendStream: read: killErr=%T %s", killErr, killErr)
			if killErr == nil {
				s.state = sendStreamClosed
				return n, s.exitErr // return the nice error
			} else {
				// we remain open so that we retry
				return n, readErr // return the normal error
			}
		}
		return n, readErr

	default:
		panic("unreachable")
	}
}

func (s *SendStream) Close() error {
	debug("sendStream: close called")
	s.mtx.Lock()
	defer s.mtx.Unlock()

	switch s.state {
	case sendStreamOpen:
		err := s.killAndWait()
		if err != nil {
			return err
		} else {
			s.state = sendStreamClosed
			return nil
		}
	case sendStreamClosed:
		return os.ErrClosed
	default:
		panic("unreachable")
	}
}

// returns nil iff the child process is gone (has been successfully waited upon)
// in that case, s.exitErr is set
func (s *SendStream) killAndWait() error {

	debug("sendStream: killAndWait enter")
	defer debug("sendStream: killAndWait leave")

	// send SIGKILL
	s.kill()

	// Close our read-end of the pipe.
	//
	// We must do this before .Wait() because in some (not all) versions/build configs of ZFS,
	// `zfs send` uses a separate kernel thread (taskq) to write the send stream (function `dump_bytes`).
	// The `zfs send` thread then waits uinterruptably for the taskq thread to finish the write.
	// And signalling the `zfs send` thread doesn't propagate to the taskq thread.
	// So we end up in a state where we .Wait() forever.
	// (See https://github.com/openzfs/zfs/issues/12500 and
	//  https://github.com/zrepl/zrepl/issues/495#issuecomment-902530043)
	//
	// By closing our read end of the pipe before .Wait(), we unblock the taskq thread if there is any.
	// If there is no separate taskq thread, the SIGKILL to `zfs end` would suffice and be most precise,
	// but due to the circumstances above, there is no other portable & robust way.
	//
	// However, the fallout from closing the pipe is that (in non-taskq builds) `zfs sends` will get a SIGPIPE.
	// And on Linux, that SIGPIPE appears to win over the previously issued SIGKILL.
	// And thus, on Linux, the `zfs send` will be killed by the default SIGPIPE handler.
	// We can observe this in the WaitStatus below.
	// This behavior is slightly annoying because the *exec.ExitError's message ("signal: broken pipe")
	// isn't as clear as ("signal: killed").
	// However, it seems like we just have to live with that. (covered by platformtest)
	var closePipeErr error
	if s.stdoutReader != nil {
		closePipeErr = s.stdoutReader.Close()
		if closePipeErr == nil {
			// avoid double-closes in case waiting below doesn't work
			// and someone attempts Close again
			s.stdoutReader = nil
		} else {
			return closePipeErr
		}
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

	// invariant: at this point, the child is gone and we cleaned up everything related to the SendStream

	if exitErr != nil {
		// zfs send exited with an error or was killed by a signal.
		s.exitErr = &ZFSError{
			Stderr:  []byte(s.stderrBuf.String()),
			WaitErr: exitErr,
		}
	} else {
		// zfs send exited successfully (we know that since waitErr was either nil or wasn't an *exec.ExitError)
		s.exitErr = nil
	}

	return nil
}

func (s *SendStream) TestOnly_ReplaceStdoutReader(f io.ReadCloser) (prev io.ReadCloser) {
	prev = s.stdoutReader
	s.stdoutReader = f
	return prev
}

func (s *SendStream) TestOnly_ExitErr() *ZFSError { return s.exitErr }

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

	realVersion, err := ZFSGetFilesystemVersion(ctx, a.FullPath(fs))
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

// When updating this struct, check Validate and ValidateCorrespondsToResumeToken (POTENTIALLY SECURITY SENSITIVE)
type ZFSSendArgsUnvalidated struct {
	FS       string
	From, To *ZFSSendArgVersion // From may be nil
	ZFSSendFlags
}

type ZFSSendArgsValidated struct {
	ZFSSendArgsUnvalidated
	FromVersion *FilesystemVersion
	ToVersion   FilesystemVersion
}

type ZFSSendFlags struct {
	Encrypted        *nodefault.Bool
	Properties       bool
	BackupProperties bool
	Raw              bool
	LargeBlocks      bool
	Compressed       bool
	EmbeddedData     bool
	Saved            bool

	// Preferred if not empty
	ResumeToken string // if not nil, must match what is specified in From, To (covered by ValidateCorrespondsToResumeToken)
}

type zfsSendArgsValidationContext struct {
	encEnabled *nodefault.Bool
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

	if err := a.ZFSSendFlags.Validate(); err != nil {
		return v, newGenericValidationError(a, errors.Wrap(err, "send flags invalid"))
	}

	valCtx := &zfsSendArgsValidationContext{}
	fsEncrypted, err := ZFSGetEncryptionEnabled(ctx, a.FS)
	if err != nil {
		return v, newValidationError(a, ZFSSendArgsFSEncryptionCheckFail,
			errors.Wrapf(err, "cannot check whether filesystem %q is encrypted", a.FS))
	}
	valCtx.encEnabled = &nodefault.Bool{B: fsEncrypted}

	if a.Encrypted.B && !fsEncrypted {
		return v, newValidationError(a, ZFSSendArgsEncryptedSendRequestedButFSUnencrypted,
			errors.Errorf("encrypted send requested, but filesystem %q is not encrypted", a.FS))
	}

	if err := a.validateEncryptionFlagsCorrespondToResumeToken(ctx, valCtx); err != nil {
		return v, newValidationError(a, ZFSSendArgsResumeTokenMismatch, err)
	}

	return ZFSSendArgsValidated{
		ZFSSendArgsUnvalidated: a,
		FromVersion:            fromVersion,
		ToVersion:              toVersion,
	}, nil
}

func (f ZFSSendFlags) Validate() error {
	if err := f.Encrypted.ValidateNoDefault(); err != nil {
		return errors.Wrap(err, "flag `Encrypted` invalid")
	}
	return nil
}

// If ResumeToken is empty, builds a command line with the flags specified.
// If ResumeToken is not empty, build a command line with just `-t {{.ResumeToken}}`.
//
// SECURITY SENSITIVE it is the caller's responsibility to ensure that a.Encrypted semantics
// hold for the file system that will be sent with the send flags returned by this function
func (a ZFSSendFlags) buildSendFlagsUnchecked() []string {

	args := make([]string, 0)

	// ResumeToken takes precedence, we assume that it has been validated
	// to reflect what is described by the other fields.
	if a.ResumeToken != "" {
		args = append(args, "-t", a.ResumeToken)
		return args
	}

	if a.Encrypted.B || a.Raw {
		args = append(args, "-w")
	}

	if a.Properties {
		args = append(args, "-p")
	}

	if a.BackupProperties {
		args = append(args, "-b")
	}

	if a.LargeBlocks {
		args = append(args, "-L")
	}

	if a.Compressed {
		args = append(args, "-c")
	}

	if a.EmbeddedData {
		args = append(args, "-e")
	}

	if a.Saved {
		args = append(args, "-S")
	}

	return args
}

func (a ZFSSendArgsValidated) buildSendCommandLine() ([]string, error) {

	flags := a.buildSendFlagsUnchecked()

	if a.ZFSSendFlags.ResumeToken != "" {
		return flags, nil
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
		flags = append(flags, toV)
	} else {
		flags = append(flags, "-i", fromV, toV)
	}
	return flags, nil
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

// Validate that the encryption settings specified in `a` correspond to the encryption settings encoded in the resume token.
//
// This is SECURITY SENSITIVE:
// It is possible for an attacker to craft arbitrary resume tokens.
// Those malicious resume tokens could encode different parameters in the resume token than expected:
// for example, they may specify another file system (e.g. the filesystem with secret data) or request unencrypted send instead of encrypted raw send.
//
// Note that we don't check correspondence of all other send flags because
// a) the resume token does not capture all send flags (e.g. send -p is implemented in libzfs and thus not represented in the resume token)
// b) it would force us to either reject resume tokens with unknown flags.
func (a ZFSSendArgsUnvalidated) validateEncryptionFlagsCorrespondToResumeToken(ctx context.Context, valCtx *zfsSendArgsValidationContext) error {

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

	// If From is set, it must match.
	if (a.From != nil) != t.HasFromGUID { // existence must be same
		if t.HasFromGUID {
			return gen.fmt("resume token not expected to be incremental, but `fromguid` = %v", t.FromGUID)
		} else {
			return gen.fmt("resume token expected to be incremental, but `fromguid` not present")
		}
	} else if t.HasFromGUID { // if exists (which is same, we checked above), they must match
		if t.FromGUID != a.From.GUID {
			return gen.fmt("resume token `fromguid` != expected: %v != %v", t.FromGUID, a.From.GUID)
		}
	} else {
		_ = struct{}{} // both empty, ok
	}

	// To must never be empty
	if !t.HasToGUID {
		return gen.fmt("resume token does not have `toguid`")
	}
	if t.ToGUID != a.To.GUID { // a.To != nil because Validate checks for that
		return gen.fmt("resume token `toguid` != expected: %v != %v", t.ToGUID, a.To.GUID)
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
func ZFSSend(ctx context.Context, sendArgs ZFSSendArgsValidated) (*SendStream, error) {

	args := make([]string, 0)
	args = append(args, "send")

	// pre-validation of sendArgs for plain ErrEncryptedSendNotSupported error
	// we tie BackupProperties (send -b) and SendRaw (-w, same as with Encrypted) to this
	// since these were released together.
	if sendArgs.Encrypted.B || sendArgs.Raw || sendArgs.BackupProperties {
		encryptionSupported, err := EncryptionCLISupported(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "cannot determine CLI native encryption support")
		}

		if !encryptionSupported {
			return nil, ErrEncryptedSendNotSupported
		}
	}

	sargs, err := sendArgs.buildSendCommandLine()
	if err != nil {
		return nil, err
	}
	args = append(args, sargs...)

	ctx, cancel := context.WithCancel(ctx)

	// setup stdout with an os.Pipe to control pipe buffer size
	stdoutReader, stdoutWriter, err := pipeWithCapacityHint(getPipeCapacityHint("ZFS_SEND_PIPE_CAPACITY_HINT"))
	if err != nil {
		cancel()
		return nil, err
	}
	stderrBuf := circlog.MustNewCircularLog(zfsSendStderrCaptureMaxSize)

	cmd := zfscmd.CommandContext(ctx, ZFS_BINARY, args...)
	cmd.SetStdio(zfscmd.Stdio{
		Stdin:  nil,
		Stdout: stdoutWriter,
		Stderr: stderrBuf,
	})

	if err := cmd.Start(); err != nil {
		cancel()
		stdoutWriter.Close()
		stdoutReader.Close()
		return nil, errors.Wrap(err, "cannot start zfs send command")
	}
	// close our writing-end of the pipe so that we don't wait for ourselves when reading from the reading  end
	stdoutWriter.Close()

	stream := &SendStream{
		cmd:          cmd,
		kill:         cancel,
		stdoutReader: stdoutReader,
		stderrBuf:    stderrBuf,
	}

	return stream, nil
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
	SizeEstimate uint64 // 0 if size estimate is not possible
}

var (
	// keep same number of capture groups for unmarshalInfoLine homogeneity

	sendDryRunInfoLineRegexFull = regexp.MustCompile(`^(?P<type>full)\t()(?P<to>[^\t]+@[^\t]+)(\t(?P<size>[0-9]+))?$`)
	// cannot enforce '[#@]' in incremental source, see test cases
	sendDryRunInfoLineRegexIncremental = regexp.MustCompile(`^(?P<type>incremental)\t(?P<from>[^\t]+)\t(?P<to>[^\t]+@[^\t]+)(\t(?P<size>[0-9]+))?$`)
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
	var matchingExpr *regexp.Regexp
	var m []string
	if mFull == nil && mInc == nil {
		return false, nil
	} else if mFull != nil && mInc != nil {
		panic(fmt.Sprintf("ambiguous ZFS dry send output: %q", l))
	} else if mFull != nil {
		matchingExpr, m = sendDryRunInfoLineRegexFull, mFull
	} else if mInc != nil {
		matchingExpr, m = sendDryRunInfoLineRegexIncremental, mInc
	}

	fields := make(map[string]string, matchingExpr.NumSubexp())
	for i, name := range matchingExpr.SubexpNames() {
		if i != 0 {
			fields[name] = m[i]
		}
	}

	s.Type, err = DrySendTypeFromString(fields["type"])
	if err != nil {
		return true, err
	}

	s.From = fields["from"]
	s.To = fields["to"]
	toFS, _, _, err := DecomposeVersionString(s.To)
	if err != nil {
		return true, fmt.Errorf("'to' is not a valid filesystem version: %s", err)
	}
	s.Filesystem = toFS

	if fields["size"] == "" {
		// workaround for OpenZFS 0.7 prior to https://github.com/openzfs/zfs/commit/835db58592d7d947e5818eb7281882e2a46073e0#diff-66bd524398bcd2ac70d90925ab6d8073L1245
		// see https://github.com/zrepl/zrepl/issues/289
		fields["size"] = "0"
	}
	s.SizeEstimate, err = strconv.ParseUint(fields["size"], 10, 64)
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
		 * XXX feature check & support this as well
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
			SizeEstimate: 0}, nil
	}

	args := make([]string, 0)
	args = append(args, "send", "-n", "-v", "-P")
	sargs, err := sendArgs.buildSendCommandLine()
	if err != nil {
		return nil, err
	}
	args = append(args, sargs...)

	cmd := zfscmd.CommandContext(ctx, ZFS_BINARY, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, &ZFSError{output, err}
	}
	var si DrySendInfo
	if err := si.unmarshalZFSOutput(output); err != nil {
		return nil, fmt.Errorf("could not parse zfs send -n output: %s", err)
	}

	// There is a bug in OpenZFS where it estimates the size incorrectly.
	// - zrepl: https://github.com/zrepl/zrepl/issues/463
	// - resulting upstream bug: https://github.com/openzfs/zfs/issues/12265
	//
	// The wrong estimates are easy to detect because they are absurdly large.
	// NB: we're doing the workaround for this late so that the test cases are not affected.
	sizeEstimateThreshold := envconst.Uint64("ZREPL_ZFS_SEND_SIZE_ESTIMATE_INCORRECT_THRESHOLD", math.MaxInt64)
	if sizeEstimateThreshold != 0 && si.SizeEstimate >= sizeEstimateThreshold {
		debug("size estimate exceeds threshold %v, working around it: %#v %q", sizeEstimateThreshold, si, args)
		si.SizeEstimate = 0
	}

	return &si, nil
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

type RecvOptions struct {
	// Rollback to the oldest snapshot, destroy it, then perform `recv -F`.
	// Note that this doesn't change property values, i.e. an existing local property value will be kept.
	RollbackAndForceRecv bool
	// Set -s flag used for resumable send & recv
	SavePartialRecvState bool

	InheritProperties  []zfsprop.Property
	OverrideProperties map[zfsprop.Property]string
}

func (opts RecvOptions) buildRecvFlags() []string {
	args := make([]string, 0)
	if opts.RollbackAndForceRecv {
		args = append(args, "-F")
	}
	if opts.SavePartialRecvState {
		args = append(args, "-s")
	}
	if opts.InheritProperties != nil {
		for _, prop := range opts.InheritProperties {
			args = append(args, "-x", string(prop))
		}
	}
	if opts.OverrideProperties != nil {
		for prop, value := range opts.OverrideProperties {
			args = append(args, "-o", fmt.Sprintf("%s=%s", prop, value))
		}
	}

	return args
}

const RecvStderrBufSiz = 1 << 15

func ZFSRecv(ctx context.Context, fs string, v *ZFSSendArgVersion, stream io.ReadCloser, opts RecvOptions) (err error) {

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
		snaps, err := ZFSListFilesystemVersions(ctx, fsdp, ListFilesystemVersionsOptions{
			Types: Snapshots,
		})
		if _, ok := err.(*DatasetDoesNotExist); ok {
			snaps = []FilesystemVersion{}
		} else if err != nil {
			return fmt.Errorf("cannot list versions for rollback for forced receive: %s", err)
		}
		sort.Slice(snaps, func(i, j int) bool {
			return snaps[i].CreateTXG < snaps[j].CreateTXG
		})
		// bookmarks are rolled back automatically
		if len(snaps) > 0 {
			// use rollback to efficiently destroy all but the earliest snapshot
			// then destroy that earliest snapshot
			// afterwards, `recv -F` will work
			rollbackTarget := snaps[0]
			rollbackTargetAbs := rollbackTarget.ToAbsPath(fsdp)
			debug("recv: rollback to %q", rollbackTargetAbs)
			if err := ZFSRollback(ctx, fsdp, rollbackTarget, "-r"); err != nil {
				return fmt.Errorf("cannot rollback %s to %s for forced receive: %s", fsdp.ToString(), rollbackTarget, err)
			}
			debug("recv: destroy %q", rollbackTargetAbs)
			if err := ZFSDestroy(ctx, rollbackTargetAbs); err != nil {
				return fmt.Errorf("cannot destroy %s for forced receive: %s", rollbackTargetAbs, err)
			}
		}
	}

	if opts.SavePartialRecvState {
		if supported, err := ResumeRecvSupported(ctx, fsdp); err != nil || !supported {
			return &ErrRecvResumeNotSupported{FS: fs, CheckErr: err}
		}
	}

	args := []string{"recv"}
	args = append(args, opts.buildRecvFlags()...)
	args = append(args, v.FullPath(fs))

	ctx, cancelCmd := context.WithCancel(ctx)
	defer cancelCmd()
	cmd := zfscmd.CommandContext(ctx, ZFS_BINARY, args...)

	// TODO report bug upstream
	// Setup an unused stdout buffer.
	// Otherwise, ZoL v0.6.5.9-1 3.16.0-4-amd64 writes the following error to stderr and exits with code 1
	//   cannot receive new filesystem stream: invalid backup stream
	stdout := bytes.NewBuffer(make([]byte, 0, 1024))

	stderr := bytes.NewBuffer(make([]byte, 0, RecvStderrBufSiz))

	stdin, stdinWriter, err := pipeWithCapacityHint(getPipeCapacityHint("ZFS_RECV_PIPE_CAPACITY_HINT"))
	if err != nil {
		return err
	}

	cmd.SetStdio(zfscmd.Stdio{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
	})

	if err = cmd.Start(); err != nil {
		stdinWriter.Close()
		stdin.Close()
		return err
	}
	stdin.Close()
	defer stdinWriter.Close()

	pid := cmd.Process().Pid
	debug := func(format string, args ...interface{}) {
		debug("recv: pid=%v: %s", pid, fmt.Sprintf(format, args...))
	}

	debug("started")

	copierErrChan := make(chan error)
	go func() {
		_, err := io.Copy(stdinWriter, stream)
		copierErrChan <- err
		stdinWriter.Close()
	}()
	waitErrChan := make(chan error)
	go func() {
		defer close(waitErrChan)
		if err = cmd.Wait(); err != nil {
			if rtErr := tryRecvErrorWithResumeToken(ctx, stderr.String()); rtErr != nil {
				waitErrChan <- rtErr
			} else if owErr := tryRecvDestroyOrOverwriteEncryptedErr(stderr.Bytes()); owErr != nil {
				waitErrChan <- owErr
			} else if readErr := tryRecvCannotReadFromStreamErr(stderr.Bytes()); readErr != nil {
				waitErrChan <- readErr
			} else {
				waitErrChan <- &ZFSError{
					Stderr:  stderr.Bytes(),
					WaitErr: err,
				}
			}
			return
		}
	}()

	copierErr := <-copierErrChan
	debug("copierErr: %T %s", copierErr, copierErr)
	if copierErr != nil {
		debug("killing zfs recv command after copierErr")
		cancelCmd()
	}

	waitErr := <-waitErrChan
	debug("waitErr: %T %s", waitErr, waitErr)

	if copierErr == nil && waitErr == nil {
		return nil
	} else if _, isReadErr := waitErr.(*RecvCannotReadFromStreamErr); isReadErr {
		return copierErr // likely network error reading from stream
	} else {
		return waitErr // almost always more interesting info. NOTE: do not wrap!
	}
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

type RecvDestroyOrOverwriteEncryptedErr struct {
	Msg string
}

func (e *RecvDestroyOrOverwriteEncryptedErr) Error() string {
	return e.Msg
}

var recvDestroyOrOverwriteEncryptedErrRe = regexp.MustCompile(`^(cannot receive new filesystem stream: zfs receive -F cannot be used to destroy an encrypted filesystem or overwrite an unencrypted one with an encrypted one)`)

func tryRecvDestroyOrOverwriteEncryptedErr(stderr []byte) *RecvDestroyOrOverwriteEncryptedErr {
	debug("tryRecvDestroyOrOverwriteEncryptedErr: %v", stderr)
	m := recvDestroyOrOverwriteEncryptedErrRe.FindSubmatch(stderr)
	if m == nil {
		return nil
	}
	return &RecvDestroyOrOverwriteEncryptedErr{Msg: string(m[1])}
}

type RecvCannotReadFromStreamErr struct {
	Msg string
}

func (e *RecvCannotReadFromStreamErr) Error() string {
	return e.Msg
}

var reRecvCannotReadFromStreamErr = regexp.MustCompile(`^(cannot receive: failed to read from stream)$`)

func tryRecvCannotReadFromStreamErr(stderr []byte) *RecvCannotReadFromStreamErr {
	m := reRecvCannotReadFromStreamErr.FindSubmatch(stderr)
	if m == nil {
		return nil
	}
	return &RecvCannotReadFromStreamErr{Msg: string(m[1])}
}

type ClearResumeTokenError struct {
	ZFSOutput []byte
	CmdError  error
}

func (e ClearResumeTokenError) Error() string {
	return fmt.Sprintf("could not clear resume token: %q", string(e.ZFSOutput))
}

// always returns *ClearResumeTokenError
func ZFSRecvClearResumeToken(ctx context.Context, fs string) (err error) {
	if err := validateZFSFilesystem(fs); err != nil {
		return err
	}

	cmd := zfscmd.CommandContext(ctx, ZFS_BINARY, "recv", "-A", fs)
	o, err := cmd.CombinedOutput()
	if err != nil {
		if bytes.Contains(o, []byte("does not have any resumable receive state to abort")) {
			return nil
		}
		return &ClearResumeTokenError{o, err}
	}
	return nil
}

type PropertyValue struct {
	Value  string
	Source PropertySource
}

type ZFSProperties struct {
	m map[string]PropertyValue
}

func NewZFSProperties() *ZFSProperties {
	return &ZFSProperties{make(map[string]PropertyValue, 4)}
}

func (p *ZFSProperties) Get(key string) string {
	return p.m[key].Value
}

func (p *ZFSProperties) GetDetails(key string) PropertyValue {
	return p.m[key]
}

func zfsSet(ctx context.Context, path string, props map[string]string) error {
	args := make([]string, 0)
	args = append(args, "set")

	for prop, val := range props {
		if strings.Contains(prop, "=") {
			return errors.New("prop contains rune '=' which is the delimiter between property name and value")
		}
		args = append(args, fmt.Sprintf("%s=%s", prop, val))
	}

	args = append(args, path)

	cmd := zfscmd.CommandContext(ctx, ZFS_BINARY, args...)
	stdio, err := cmd.CombinedOutput()
	if err != nil {
		err = &ZFSError{
			Stderr:  stdio,
			WaitErr: err,
		}
	}

	return err
}

func ZFSSet(ctx context.Context, fs *DatasetPath, props map[string]string) error {
	return zfsSet(ctx, fs.ToString(), props)
}

func ZFSGet(ctx context.Context, fs *DatasetPath, props []string) (*ZFSProperties, error) {
	return zfsGet(ctx, fs.ToString(), props, SourceAny)
}

// The returned error includes requested filesystem and version as quoted strings in its error message
func ZFSGetGUID(ctx context.Context, fs string, version string) (g uint64, err error) {
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
	props, err := zfsGet(ctx, path, []string{"guid"}, SourceAny) // always local
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(props.Get("guid"), 10, 64)
}

type GetMountpointOutput struct {
	Mounted    bool
	Mountpoint string
}

func ZFSGetMountpoint(ctx context.Context, fs string) (*GetMountpointOutput, error) {
	if err := EntityNamecheck(fs, EntityTypeFilesystem); err != nil {
		return nil, err
	}
	props, err := zfsGet(ctx, fs, []string{"mountpoint", "mounted"}, SourceAny)
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

func ZFSGetRawAnySource(ctx context.Context, path string, props []string) (*ZFSProperties, error) {
	return zfsGet(ctx, path, props, SourceAny)
}

var zfsGetDatasetDoesNotExistRegexp = regexp.MustCompile(`^cannot open '([^)]+)': (dataset does not exist|no such pool or dataset)`) // verified in platformtest

type DatasetDoesNotExist struct {
	Path string
}

func (d *DatasetDoesNotExist) Error() string { return fmt.Sprintf("dataset %q does not exist", d.Path) }

func tryDatasetDoesNotExist(expectPath string, stderr []byte) *DatasetDoesNotExist {
	if sm := zfsGetDatasetDoesNotExistRegexp.FindSubmatch(stderr); sm != nil {
		if string(sm[1]) == expectPath {
			return &DatasetDoesNotExist{expectPath}
		}
	}
	return nil
}

//go:generate enumer -type=PropertySource -trimprefix=Source
type PropertySource uint32

const (
	SourceLocal PropertySource = 1 << iota
	SourceDefault
	SourceInherited
	SourceNone
	SourceTemporary
	SourceReceived

	SourceAny PropertySource = ^PropertySource(0)
)

var propertySourceParseLUT = map[string]PropertySource{
	"local":     SourceLocal,
	"default":   SourceDefault,
	"inherited": SourceInherited,
	"-":         SourceNone,
	"temporary": SourceTemporary,
	"received":  SourceReceived,
}

func parsePropertySource(s string) (PropertySource, error) {
	fields := strings.Fields(s)
	if len(fields) > 0 {
		v, ok := propertySourceParseLUT[fields[0]]
		if ok {
			return v, nil
		}
		// fallthrough
	}
	return 0, fmt.Errorf("unknown property source %q", s)
}

func (s PropertySource) zfsGetSourceFieldPrefixes() []string {
	prefixes := make([]string, 0, 7)
	if s&SourceLocal != 0 {
		prefixes = append(prefixes, "local")
	}
	if s&SourceDefault != 0 {
		prefixes = append(prefixes, "default")
	}
	if s&SourceInherited != 0 {
		prefixes = append(prefixes, "inherited")
	}
	if s&SourceNone != 0 {
		prefixes = append(prefixes, "-")
	}
	if s&SourceTemporary != 0 {
		prefixes = append(prefixes, "temporary")
	}
	if s&SourceReceived != 0 {
		prefixes = append(prefixes, "received")
	}
	if s == SourceAny {
		prefixes = append(prefixes, "")
	}
	return prefixes
}

func zfsGetRecursive(ctx context.Context, path string, depth int, dstypes []string, props []string, allowedSources PropertySource) (map[string]*ZFSProperties, error) {
	args := []string{"get", "-Hp", "-o", "name,property,value,source"}
	if depth != 0 {
		args = append(args, "-r")
		if depth != -1 {
			args = append(args, "-d", fmt.Sprintf("%d", depth))
		}
	}
	if len(dstypes) > 0 {
		args = append(args, "-t", strings.Join(dstypes, ","))
	}
	args = append(args, strings.Join(props, ","), path)
	cmd := zfscmd.CommandContext(ctx, ZFS_BINARY, args...)
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
	propsByFS := make(map[string]*ZFSProperties)
	allowedPrefixes := allowedSources.zfsGetSourceFieldPrefixes()
	for _, line := range lines[:len(lines)-1] { // last line is an empty line due to how strings.Split works
		fields := strings.FieldsFunc(line, func(r rune) bool {
			return r == '\t'
		})
		if len(fields) != 4 {
			return nil, fmt.Errorf("zfs get did not return name,property,value,source tuples")
		}
		for _, p := range allowedPrefixes {
			// prefix-match so that SourceAny (= "") works
			if strings.HasPrefix(fields[3], p) {
				source, err := parsePropertySource(fields[3])
				if err != nil {
					return nil, errors.Wrap(err, "parse property source")
				}
				fsProps, ok := propsByFS[fields[0]]
				if !ok {
					fsProps = &ZFSProperties{
						make(map[string]PropertyValue),
					}
				}
				if _, ok := fsProps.m[fields[1]]; ok {
					return nil, errors.Errorf("duplicate property %q for dataset %q", fields[1], fields[0])
				}
				fsProps.m[fields[1]] = PropertyValue{
					Value:  fields[2],
					Source: source,
				}
				propsByFS[fields[0]] = fsProps
				break
			}
		}
	}
	// validate we got expected output
	for fs, fsProps := range propsByFS {
		if len(fsProps.m) != len(props) {
			return nil, errors.Errorf("zfs get did not return all requested values for dataset %q\noutput was:\n%s", fs, o)
		}
	}
	return propsByFS, nil
}

func zfsGet(ctx context.Context, path string, props []string, allowedSources PropertySource) (*ZFSProperties, error) {
	propMap, err := zfsGetRecursive(ctx, path, 0, nil, props, allowedSources)
	if err != nil {
		return nil, err
	}
	if len(propMap) == 0 {
		// XXX callers expect to always get a result here
		// They will observe props.Get("propname") == ""
		// We should change .Get to return a tuple, or an error, or whatever.
		return &ZFSProperties{make(map[string]PropertyValue)}, nil
	}
	if len(propMap) != 1 {
		return nil, errors.Errorf("zfs get unexpectedly returned properties for multiple datasets")
	}
	res, ok := propMap[path]
	if !ok {
		return nil, errors.Errorf("zfs get returned properties for a different dataset that requested")
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

func ZFSDestroy(ctx context.Context, arg string) (err error) {

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

	cmd := zfscmd.CommandContext(ctx, ZFS_BINARY, "destroy", arg)
	stdio, err := cmd.CombinedOutput()
	if err != nil {
		err = &ZFSError{
			Stderr:  stdio,
			WaitErr: err,
		}

		if destroyOneOrMoreSnapshotsNoneExistedErrorRegexp.Match(stdio) {
			err = &DatasetDoesNotExist{arg}
		} else if match := destroyBookmarkDoesNotExist.FindStringSubmatch(string(stdio)); match != nil && match[1] == arg {
			err = &DatasetDoesNotExist{arg}
		} else if dsNotExistErr := tryDatasetDoesNotExist(filesystem, stdio); dsNotExistErr != nil {
			err = dsNotExistErr
		} else if dserr := tryParseDestroySnapshotsError(arg, stdio); dserr != nil {
			err = dserr
		}

	}

	return

}

func ZFSDestroyIdempotent(ctx context.Context, path string) error {
	err := ZFSDestroy(ctx, path)
	if _, ok := err.(*DatasetDoesNotExist); ok {
		return nil
	}
	return err
}

func ZFSSnapshot(ctx context.Context, fs *DatasetPath, name string, recursive bool) (err error) {

	promTimer := prometheus.NewTimer(prom.ZFSSnapshotDuration.WithLabelValues(fs.ToString()))
	defer promTimer.ObserveDuration()

	snapname := fmt.Sprintf("%s@%s", fs.ToString(), name)
	if err := EntityNamecheck(snapname, EntityTypeSnapshot); err != nil {
		return errors.Wrap(err, "zfs snapshot")
	}

	cmd := zfscmd.CommandContext(ctx, ZFS_BINARY, "snapshot", snapname)
	stdio, err := cmd.CombinedOutput()
	if err != nil {
		err = &ZFSError{
			Stderr:  stdio,
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
// if `v` is a bookmark, returns ErrBookmarkCloningNotSupported
// unless a bookmark with the name `bookmark` exists and has the same idenitty (zfs.FilesystemVersionEqualIdentity)
//
// v must be validated by the caller
//
func ZFSBookmark(ctx context.Context, fs string, v FilesystemVersion, bookmark string) (bm FilesystemVersion, err error) {

	bm = FilesystemVersion{
		Type:     Bookmark,
		Name:     bookmark,
		UserRefs: OptionUint64{Valid: false},
		// bookmarks have the same createtxg, guid and creation as their origin
		CreateTXG: v.CreateTXG,
		Guid:      v.Guid,
		Creation:  v.Creation,
	}

	promTimer := prometheus.NewTimer(prom.ZFSBookmarkDuration.WithLabelValues(fs))
	defer promTimer.ObserveDuration()

	bookmarkname := fmt.Sprintf("%s#%s", fs, bookmark)
	if err := EntityNamecheck(bookmarkname, EntityTypeBookmark); err != nil {
		return bm, err
	}

	if v.IsBookmark() {
		existingBm, err := ZFSGetFilesystemVersion(ctx, bookmarkname)
		if _, ok := err.(*DatasetDoesNotExist); ok {
			return bm, ErrBookmarkCloningNotSupported
		} else if err != nil {
			return bm, errors.Wrap(err, "bookmark: idempotency check for bookmark cloning")
		}
		if FilesystemVersionEqualIdentity(bm, existingBm) {
			return existingBm, nil
		}
		return bm, ErrBookmarkCloningNotSupported // TODO This is work in progress: https://github.com/zfsonlinux/zfs/pull/9571
	}

	snapname := v.FullPath(fs)
	if err := EntityNamecheck(snapname, EntityTypeSnapshot); err != nil {
		return bm, err
	}

	cmd := zfscmd.CommandContext(ctx, ZFS_BINARY, "bookmark", snapname, bookmarkname)
	stdio, err := cmd.CombinedOutput()
	if err != nil {
		if ddne := tryDatasetDoesNotExist(snapname, stdio); ddne != nil {
			return bm, ddne
		} else if zfsBookmarkExistsRegex.Match(stdio) {

			// check if this was idempotent
			bookGuid, err := ZFSGetGUID(ctx, fs, "#"+bookmark)
			if err != nil {
				return bm, errors.Wrap(err, "bookmark: idempotency check for bookmark creation") // guid error expressive enough
			}

			if v.Guid == bookGuid {
				debug("bookmark: %q %q was idempotent: {snap,book}guid %d == %d", snapname, bookmarkname, v.Guid, bookGuid)
				return bm, nil
			}
			return bm, &BookmarkExists{
				fs: fs, bookmarkOrigin: v.ToSendArgVersion(), bookmark: bookmark,
				zfsMsg:   string(stdio),
				bookGuid: bookGuid,
			}

		} else {
			return bm, &ZFSError{
				Stderr:  stdio,
				WaitErr: err,
			}
		}

	}

	return bm, nil
}

func ZFSRollback(ctx context.Context, fs *DatasetPath, snapshot FilesystemVersion, rollbackArgs ...string) (err error) {

	snapabs := snapshot.ToAbsPath(fs)
	if snapshot.Type != Snapshot {
		return fmt.Errorf("can only rollback to snapshots, got %s", snapabs)
	}

	args := []string{"rollback"}
	args = append(args, rollbackArgs...)
	args = append(args, snapabs)

	cmd := zfscmd.CommandContext(ctx, ZFS_BINARY, args...)
	stdio, err := cmd.CombinedOutput()
	if err != nil {
		err = &ZFSError{
			Stderr:  stdio,
			WaitErr: err,
		}
	}

	return err
}
