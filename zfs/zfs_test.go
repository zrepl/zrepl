package zfs

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestZFSListHandlesProducesZFSErrorOnNonZeroExit(t *testing.T) {
	t.SkipNow() // FIXME ZFS_BINARY does not work if tests run in parallel

	var err error

	ctx := context.Background()

	ZFS_BINARY = "./test_helpers/zfs_failer.sh"

	_, err = ZFSList(ctx, []string{"fictionalprop"}, "nonexistent/dataset")

	assert.Error(t, err)
	zfsError, ok := err.(*ZFSError)
	assert.True(t, ok)
	assert.Equal(t, "error: this is a mock\n", string(zfsError.Stderr))
}

func TestDatasetPathTrimNPrefixComps(t *testing.T) {
	p, err := NewDatasetPath("foo/bar/a/b")
	assert.Nil(t, err)
	p.TrimNPrefixComps(2)
	assert.True(t, p.Equal(toDatasetPath("a/b")))
	p.TrimNPrefixComps((2))
	assert.True(t, p.Empty())
	p.TrimNPrefixComps((1))
	assert.True(t, p.Empty(), "empty trimming shouldn't do harm")
}

func TestZFSPropertySource(t *testing.T) {

	tcs := []struct {
		in  zfsPropertySource
		exp []string
	}{
		{
			in: sourceAny,
			// although empty prefix matches any source
			exp: []string{"local", "default", "inherited", "-", "temporary", "received", ""},
		},
		{
			in:  sourceTemporary,
			exp: []string{"temporary"},
		},
		{
			in:  sourceLocal | sourceInherited,
			exp: []string{"local", "inherited"},
		},
	}

	toSet := func(in []string) map[string]struct{} {
		m := make(map[string]struct{}, len(in))
		for _, s := range in {
			m[s] = struct{}{}
		}
		return m
	}

	for _, tc := range tcs {

		res := tc.in.zfsGetSourceFieldPrefixes()
		resSet := toSet(res)
		expSet := toSet(tc.exp)
		assert.Equal(t, expSet, resSet)
	}

}

func TestDrySendRegexesHaveSameCaptureGroupCount(t *testing.T) {
	assert.Equal(t, sendDryRunInfoLineRegexFull.NumSubexp(), sendDryRunInfoLineRegexIncremental.NumSubexp())
}

func TestDrySendInfo(t *testing.T) {

	// # full send
	// $ zfs send -Pnv -t 1-9baebea70-b8-789c636064000310a500c4ec50360710e72765a52697303030419460caa7a515a79680647ce0f26c48f2499525a9c5405ac3c90fabfe92fcf4d2cc140686b30972c7850efd0cd24092e704cbe725e6a632305415e5e797e803cd2ad14f743084b805001b201795
	fullSend := `
resume token contents:
nvlist version: 0
	object = 0x2
	offset = 0x4c0000
	bytes = 0x4e4228
	toguid = 0x52f9c212c71e60cd
	toname = zroot/test/a@1
full	zroot/test/a@1	5389768
`

	// # incremental send with token
	// $ zfs send -nvP -t 1-ef01e717e-e0-789c636064000310a501c49c50360710a715e5e7a69766a63040c1d904b9e342877e062900d9ec48eaf293b252934b181898a0ea30e4d3d28a534b40323e70793624f9a4ca92d46220fdc1ce0fabfe927c882bc46c8a0a9f71ad3baf8124cf0996cf4bcc4d6560a82acacf2fd1079a55a29fe86004710b00d8ae1f93
	incSend := `
resume token contents:
nvlist version: 0
	fromguid = 0x52f9c212c71e60cd
	object = 0x2
	offset = 0x4c0000
	bytes = 0x4e3ef0
	toguid = 0xcfae0ae671723c16
	toname = zroot/test/a@2
incremental	zroot/test/a@1	zroot/test/a@2	5383936
`

	// # incremental send with token + bookmark
	// $ zfs send -nvP -t 1-ef01e717e-e0-789c636064000310a501c49c50360710a715e5e7a69766a63040c1d904b9e342877e062900d9ec48eaf293b252934b181898a0ea30e4d3d28a534b40323e70793624f9a4ca92d46220fdc1ce0fabfe927c882bc46c8a0a9f71ad3baf8124cf0996cf4bcc4d6560a82acacf2fd1079a55a29fe86004710b00d8ae1f93
	incSendBookmark := `
resume token contents:
nvlist version: 0
	fromguid = 0x52f9c212c71e60cd
	object = 0x2
	offset = 0x4c0000
	bytes = 0x4e3ef0
	toguid = 0xcfae0ae671723c16
	toname = zroot/test/a@2
incremental	zroot/test/a#1	zroot/test/a@2	5383312

`

	// incremental send without token
	// $ sudo zfs send -nvP -i @1 zroot/test/a@2
	incNoToken := `
incremental	1	zroot/test/a@2	10511856
size	10511856
`
	// full send without token
	// $ sudo zfs send -nvP  zroot/test/a@3
	fullNoToken := `
full	zroot/test/a@3	10518512
size	10518512
`

	fullWithSpaces := "\nfull\tpool1/otherjob/ds with spaces@blaffoo\t12912\nsize\t12912\n"
	fullWithSpacesInIntermediateComponent := "\nfull\tpool1/otherjob/another ds with spaces/childfs@blaffoo\t12912\nsize\t12912\n"
	incrementalWithSpaces := "\nincremental\tblaffoo\tpool1/otherjob/another ds with spaces@blaffoo2\t624\nsize\t624\n"
	incrementalWithSpacesInIntermediateComponent := "\nincremental\tblaffoo\tpool1/otherjob/another ds with spaces/childfs@blaffoo2\t624\nsize\t624\n"

	type tc struct {
		name   string
		in     string
		exp    *DrySendInfo
		expErr bool
	}

	tcs := []tc{
		{
			name: "fullSend", in: fullSend,
			exp: &DrySendInfo{
				Type:         DrySendTypeFull,
				Filesystem:   "zroot/test/a",
				From:         "",
				To:           "zroot/test/a@1",
				SizeEstimate: 5389768,
			},
		},
		{
			name: "incSend", in: incSend,
			exp: &DrySendInfo{
				Type:         DrySendTypeIncremental,
				Filesystem:   "zroot/test/a",
				From:         "zroot/test/a@1",
				To:           "zroot/test/a@2",
				SizeEstimate: 5383936,
			},
		},
		{
			name: "incSendBookmark", in: incSendBookmark,
			exp: &DrySendInfo{
				Type:         DrySendTypeIncremental,
				Filesystem:   "zroot/test/a",
				From:         "zroot/test/a#1",
				To:           "zroot/test/a@2",
				SizeEstimate: 5383312,
			},
		},
		{
			name: "incNoToken", in: incNoToken,
			exp: &DrySendInfo{
				Type:       DrySendTypeIncremental,
				Filesystem: "zroot/test/a",
				// as can be seen in the string incNoToken,
				// we cannot infer whether the incremental source is a snapshot or bookmark
				From:         "1", // yes, this is actually correct on ZoL 0.7.11
				To:           "zroot/test/a@2",
				SizeEstimate: 10511856,
			},
		},
		{
			name: "fullNoToken", in: fullNoToken,
			exp: &DrySendInfo{
				Type:         DrySendTypeFull,
				Filesystem:   "zroot/test/a",
				From:         "",
				To:           "zroot/test/a@3",
				SizeEstimate: 10518512,
			},
		},
		{
			name: "fullWithSpaces", in: fullWithSpaces,
			exp: &DrySendInfo{
				Type:         DrySendTypeFull,
				Filesystem:   "pool1/otherjob/ds with spaces",
				From:         "",
				To:           "pool1/otherjob/ds with spaces@blaffoo",
				SizeEstimate: 12912,
			},
		},
		{
			name: "fullWithSpacesInIntermediateComponent", in: fullWithSpacesInIntermediateComponent,
			exp: &DrySendInfo{
				Type:         DrySendTypeFull,
				Filesystem:   "pool1/otherjob/another ds with spaces/childfs",
				From:         "",
				To:           "pool1/otherjob/another ds with spaces/childfs@blaffoo",
				SizeEstimate: 12912,
			},
		},
		{
			name: "incrementalWithSpaces", in: incrementalWithSpaces,
			exp: &DrySendInfo{
				Type:         DrySendTypeIncremental,
				Filesystem:   "pool1/otherjob/another ds with spaces",
				From:         "blaffoo",
				To:           "pool1/otherjob/another ds with spaces@blaffoo2",
				SizeEstimate: 624,
			},
		},
		{
			name: "incrementalWithSpacesInIntermediateComponent", in: incrementalWithSpacesInIntermediateComponent,
			exp: &DrySendInfo{
				Type:         DrySendTypeIncremental,
				Filesystem:   "pool1/otherjob/another ds with spaces/childfs",
				From:         "blaffoo",
				To:           "pool1/otherjob/another ds with spaces/childfs@blaffoo2",
				SizeEstimate: 624,
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {

			in := tc.in[1:] // strip first newline
			var si DrySendInfo
			err := si.unmarshalZFSOutput([]byte(in))
			if tc.expErr {
				assert.Error(t, err)
			}
			t.Logf("%#v", &si)
			if tc.exp != nil {
				assert.Equal(t, tc.exp, &si)
			}

		})
	}
}

func TestTryRecvDestroyOrOverwriteEncryptedErr(t *testing.T) {
	msg := "cannot receive new filesystem stream: zfs receive -F cannot be used to destroy an encrypted filesystem or overwrite an unencrypted one with an encrypted one\n"
	assert.GreaterOrEqual(t, RecvStderrBufSiz, len(msg))

	err := tryRecvDestroyOrOverwriteEncryptedErr([]byte(msg))
	require.NotNil(t, err)
	assert.EqualError(t, err, strings.TrimSpace(msg))
}
