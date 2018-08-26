package zfs

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"strconv"
	"time"
)

type ResumeToken struct {
	HasFromGUID, HasToGUID bool
	FromGUID, ToGUID       uint64
	// no support for other fields
}

var resumeTokenNVListRE = regexp.MustCompile(`\t(\S+) = (.*)`)
var resumeTokenContentsRE = regexp.MustCompile(`resume token contents:\nnvlist version: 0`)
var resumeTokenIsCorruptRE = regexp.MustCompile(`resume token is corrupt`)

var ResumeTokenCorruptError = errors.New("resume token is corrupt")
var ResumeTokenDecodingNotSupported = errors.New("zfs binary does not allow decoding resume token or zrepl cannot scrape zfs output")
var ResumeTokenParsingError = errors.New("zrepl cannot parse resume token values")

// Abuse 'zfs send' to decode the resume token
//
// FIXME: implement nvlist unpacking in Go and read through libzfs_sendrecv.c
func ParseResumeToken(ctx context.Context, token string) (*ResumeToken, error) {

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

	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
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
		}
	}

	if !rt.HasToGUID {
		return nil, ResumeTokenDecodingNotSupported
	}

	return rt, nil

}

func ZFSGetReceiveResumeToken(fs *DatasetPath) (string, error) {
	const prop_receive_resume_token = "receive_resume_token"
	props, err := ZFSGet(fs, []string{prop_receive_resume_token})
	if err != nil {
		return "", err
	}
	res := props.m[prop_receive_resume_token]
	if res == "-" {
		return "", nil
	} else {
		return res, nil
	}

}
