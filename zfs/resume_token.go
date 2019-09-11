package zfs

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/util/envconst"
)

// NOTE: Update ZFSSendARgs.Validate when changning fields (potentially SECURITY SENSITIVE)
type ResumeToken struct {
	HasFromGUID, HasToGUID    bool
	FromGUID, ToGUID          uint64
	ToName                    string
	HasCompressOK, CompressOK bool
	HasRawOk, RawOK           bool
}

var resumeTokenNVListRE = regexp.MustCompile(`\t(\S+) = (.*)`)
var resumeTokenContentsRE = regexp.MustCompile(`resume token contents:\nnvlist version: 0`)
var resumeTokenIsCorruptRE = regexp.MustCompile(`resume token is corrupt`)

var ResumeTokenCorruptError = errors.New("resume token is corrupt")
var ResumeTokenDecodingNotSupported = errors.New("zfs binary does not allow decoding resume token or zrepl cannot scrape zfs output")
var ResumeTokenParsingError = errors.New("zrepl cannot parse resume token values")

var resumeSendSupportedCheck struct {
	once      sync.Once
	supported bool
	err       error
}

func ResumeSendSupported() (bool, error) {
	resumeSendSupportedCheck.once.Do(func() {
		// "feature discovery"
		cmd := exec.Command("zfs", "send")
		output, err := cmd.CombinedOutput()
		if ee, ok := err.(*exec.ExitError); !ok || ok && !ee.Exited() {
			resumeSendSupportedCheck.err = errors.Wrap(err, "resumable send cli support feature check failed")
		}
		def := strings.Contains(string(output), "receive_resume_token")
		resumeSendSupportedCheck.supported = envconst.Bool("ZREPL_EXPERIMENTAL_ZFS_SEND_RESUME_SUPPORTED", def)
		debug("resume send feature check complete %#v", &resumeSendSupportedCheck)
	})
	return resumeSendSupportedCheck.supported, resumeSendSupportedCheck.err
}

var resumeRecvPoolSupportRecheckTimeout = envconst.Duration("ZREPL_ZFS_RESUME_RECV_POOL_SUPPORT_RECHECK_TIMEOUT", 30*time.Second)

type resumeRecvPoolSupportedResult struct {
	lastCheck time.Time
	supported bool
	err       error
}

var resumeRecvSupportedCheck struct {
	mtx         sync.RWMutex
	flagSupport struct {
		checked   bool
		supported bool
		err       error
	}
	poolSupported map[string]resumeRecvPoolSupportedResult
}

// fs == nil only checks for CLI support
func ResumeRecvSupported(ctx context.Context, fs *DatasetPath) (bool, error) {
	sup := &resumeRecvSupportedCheck
	sup.mtx.RLock()
	defer sup.mtx.RUnlock()
	upgradeWhile := func(cb func()) {
		sup.mtx.RUnlock()
		defer sup.mtx.RLock()
		sup.mtx.Lock()
		defer sup.mtx.Unlock()
		cb()
	}

	if !sup.flagSupport.checked {
		output, err := exec.CommandContext(ctx, "zfs", "receive").CombinedOutput()
		upgradeWhile(func() {
			sup.flagSupport.checked = true
			if ee, ok := err.(*exec.ExitError); err != nil && (!ok || ok && !ee.Exited()) {
				sup.flagSupport.err = err
			} else {
				sup.flagSupport.supported = strings.Contains(string(output), "-A <filesystem|volume>")
			}
			debug("resume recv cli flag feature check result: %#v", sup.flagSupport)
		})
		// fallthrough
	}

	if sup.flagSupport.err != nil {
		return false, errors.Wrap(sup.flagSupport.err, "zfs recv feature check for resumable send & recv failed")
	} else if !sup.flagSupport.supported || fs == nil {
		return sup.flagSupport.supported, nil
	}

	// Flag is supported and pool-support is request
	// Now check for pool support

	pool, err := fs.Pool()
	if err != nil {
		return false, errors.Wrap(err, "resume recv check requires pool of dataset")
	}

	if sup.poolSupported == nil {
		upgradeWhile(func() {
			sup.poolSupported = make(map[string]resumeRecvPoolSupportedResult)
		})
	}

	var poolSup resumeRecvPoolSupportedResult
	var ok bool
	if poolSup, ok = sup.poolSupported[pool]; !ok || // shadow
		(!poolSup.supported && time.Since(poolSup.lastCheck) > resumeRecvPoolSupportRecheckTimeout) {

		output, err := exec.CommandContext(ctx, "zpool", "get", "-H", "-p", "-o", "value", "feature@extensible_dataset", pool).CombinedOutput()
		if err != nil {
			debug("resume recv pool support check result: %#v", sup.flagSupport)
			poolSup.supported = false
			poolSup.err = err
		} else {
			poolSup.err = nil
			o := strings.TrimSpace(string(output))
			poolSup.supported = o == "active" || o == "enabled"
		}
		poolSup.lastCheck = time.Now()

		// we take the lock late, so two updaters might check simultaneously, but that shouldn't hurt
		upgradeWhile(func() {
			sup.poolSupported[pool] = poolSup
		})
		// fallthrough
	}

	if poolSup.err != nil {
		return false, errors.Wrapf(poolSup.err, "pool %q check for feature@extensible_dataset feature failed", pool)
	}

	return poolSup.supported, nil
}

// Abuse 'zfs send' to decode the resume token
//
// FIXME: implement nvlist unpacking in Go and read through libzfs_sendrecv.c
func ParseResumeToken(ctx context.Context, token string) (*ResumeToken, error) {

	if supported, err := ResumeSendSupported(); err != nil {
		return nil, err
	} else if !supported {
		return nil, ResumeTokenDecodingNotSupported
	}

	// Example resume tokens:
	//
	// From a non-incremental send
	// 1-bf31b879a-b8-789c636064000310a500c4ec50360710e72765a5269740f80cd8e4d3d28a534b18e00024cf86249f5459925acc802a8facbf243fbd3433858161f5ddb9ab1ae7c7466a20c97382e5f312735319180af2f3730cf58166953824c2cc0200cde81651

	// From an incremental send
	// 1-c49b979a2-e0-789c636064000310a501c49c50360710a715e5e7a69766a63040c1eabb735735ce8f8d5400b2d991d4e52765a5269740f82080219f96569c5ac2000720793624f9a4ca92d46206547964fd25f91057f09e37babb88c9bf5503499e132c9f97989bcac050909f9f63a80f34abc421096616007c881d4c

	// Resulting output of zfs send -nvt <token>
	//
	//resume token contents:
	//nvlist version: 0
	//	fromguid = 0x595d9f81aa9dddab
	//	object = 0x1
	//	offset = 0x0
	//	bytes = 0x0
	//	toguid = 0x854f02a2dd32cf0d
	//	toname = pool1/test@b
	//cannot resume send: 'pool1/test@b' used in the initial send no longer exists

	cmd := exec.CommandContext(ctx, ZFS_BINARY, "send", "-nvt", string(token))
	output, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if !exitErr.Exited() {
				return nil, err
			}
			// we abuse zfs send for decoding, the exit error may be due to
			// a) the token being from a third machine
			// b) it no longer exists on the machine where
		} else {
			return nil, err
		}
	}

	if !resumeTokenContentsRE.Match(output) {
		if resumeTokenIsCorruptRE.Match(output) {
			return nil, ResumeTokenCorruptError
		}
		return nil, ResumeTokenDecodingNotSupported
	}

	matches := resumeTokenNVListRE.FindAllStringSubmatch(string(output), -1)
	if matches == nil {
		return nil, ResumeTokenDecodingNotSupported
	}

	rt := &ResumeToken{}

	for _, m := range matches {
		attr, val := m[1], m[2]
		switch attr {
		case "fromguid":
			rt.FromGUID, err = strconv.ParseUint(val, 0, 64)
			if err != nil {
				return nil, ResumeTokenParsingError
			}
			rt.HasFromGUID = true
		case "toguid":
			rt.ToGUID, err = strconv.ParseUint(val, 0, 64)
			if err != nil {
				return nil, ResumeTokenParsingError
			}
			rt.HasToGUID = true
		case "toname":
			rt.ToName = val
		case "rawok":
			rt.HasRawOk = true
			rt.RawOK, err = strconv.ParseBool(val)
			if err != nil {
				return nil, ResumeTokenParsingError
			}
		case "compressok":
			rt.HasCompressOK = true
			rt.CompressOK, err = strconv.ParseBool(val)
			if err != nil {
				return nil, ResumeTokenParsingError
			}
		}
	}

	if !rt.HasToGUID {
		return nil, ResumeTokenDecodingNotSupported
	}

	return rt, nil

}

// if string is empty and err == nil, the feature is not supported
func ZFSGetReceiveResumeTokenOrEmptyStringIfNotSupported(ctx context.Context, fs *DatasetPath) (string, error) {
	if supported, err := ResumeRecvSupported(ctx, fs); err != nil {
		return "", errors.Wrap(err, "cannot determine zfs recv resume support")
	} else if !supported {
		return "", nil
	}
	const prop_receive_resume_token = "receive_resume_token"
	props, err := ZFSGet(fs, []string{prop_receive_resume_token})
	if err != nil {
		return "", err
	}
	res := props.Get(prop_receive_resume_token)
	debug("%q receive_resume_token=%q", fs.ToString(), res)
	if res == "-" {
		return "", nil
	} else {
		return res, nil
	}
}

func (t *ResumeToken) ToNameSplit() (fs *DatasetPath, snapName string, err error) {
	comps := strings.SplitN(t.ToName, "@", 2)
	if len(comps) != 2 {
		return nil, "", fmt.Errorf("resume token field `toname` does not contain @: %q", t.ToName)
	}
	dp, err := NewDatasetPath(comps[0])
	if err != nil {
		return nil, "", errors.Wrap(err, "resume token field `toname` dataset path invalid")
	}
	return dp, comps[1], nil
}
