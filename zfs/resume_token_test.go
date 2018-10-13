package zfs_test

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/zrepl/zrepl/zfs"
	"testing"
)

type ResumeTokenTest struct {
	Msg         string
	Token       string
	ExpectToken *zfs.ResumeToken
	ExpectError error
}

func (rtt *ResumeTokenTest) Test(t *testing.T) {
	t.Log(rtt.Msg)
	res, err := zfs.ParseResumeToken(context.TODO(), rtt.Token)

	if rtt.ExpectError != nil {
		assert.EqualValues(t, rtt.ExpectError, err)
		return
	}
	if rtt.ExpectToken != nil {
		assert.Nil(t, err)
		assert.EqualValues(t, rtt.ExpectToken, res)
		return
	}
}

func TestParseResumeToken(t *testing.T) {

	t.SkipNow() // FIXME not compatible with docker

	tbl := []ResumeTokenTest{
		{
			Msg:   "normal send (non-incremental)",
			Token: `1-bf31b879a-b8-789c636064000310a500c4ec50360710e72765a5269740f80cd8e4d3d28a534b18e00024cf86249f5459925acc802a8facbf243fbd3433858161f5ddb9ab1ae7c7466a20c97382e5f312735319180af2f3730cf58166953824c2cc0200cde81651`,
			ExpectToken: &zfs.ResumeToken{
				HasToGUID: true,
				ToGUID:    0x595d9f81aa9dddab,
			},
		},
		{
			Msg:   "normal send (incremental)",
			Token: `1-c49b979a2-e0-789c636064000310a501c49c50360710a715e5e7a69766a63040c1eabb735735ce8f8d5400b2d991d4e52765a5269740f82080219f96569c5ac2000720793624f9a4ca92d46206547964fd25f91057f09e37babb88c9bf5503499e132c9f97989bcac050909f9f63a80f34abc421096616007c881d4c`,
			ExpectToken: &zfs.ResumeToken{
				HasToGUID:   true,
				ToGUID:      0x854f02a2dd32cf0d,
				HasFromGUID: true,
				FromGUID:    0x595d9f81aa9dddab,
			},
		},
		{
			Msg:         "corrupted token",
			Token:       `1-bf31b879a-b8-789c636064000310a500c4ec50360710e72765a5269740f80cd8e4d3d28a534b18e00024cf86249f5459925acc802a8facbf243fbd3433858161f5ddb9ab1ae7c7466a20c97382e5f312735319180af2f3730cf58166953824c2cc0200cd12345`,
			ExpectError: zfs.ResumeTokenCorruptError,
		},
	}

	for _, test := range tbl {
		test.Test(t)
	}

}
