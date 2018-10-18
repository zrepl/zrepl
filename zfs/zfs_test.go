package zfs

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestZFSListHandlesProducesZFSErrorOnNonZeroExit(t *testing.T) {
	t.SkipNow() // FIXME ZFS_BINARY does not work if tests run in parallel

	var err error

	ZFS_BINARY = "./test_helpers/zfs_failer.sh"

	_, err = ZFSList([]string{"fictionalprop"}, "nonexistent/dataset")

	assert.Error(t, err)
	zfsError, ok := err.(ZFSError)
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

	tcs := []struct{
		in zfsPropertySource
		exp []string
	}{
		{
			in: sourceAny,
			exp: []string{"local", "default", "inherited", "-", "temporary"},
		},
		{
			in: sourceTemporary,
			exp: []string{"temporary"},
		},
		{
			in: sourceLocal|sourceInherited,
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

	// # incremental send with token + bookmarmk
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

	type tc struct {
		name string
		in string
		exp *DrySendInfo
		expErr bool
	}

	tcs := []tc{
		{
			name: "fullSend", in: fullSend,
			exp: &DrySendInfo{
				Type: DrySendTypeFull,
				From: "",
				To: "zroot/test/a@1",
				SizeEstimate: 5389768,
			},
		},
		{
			name: "incSend", in: incSend,
			exp: &DrySendInfo{
				Type:         DrySendTypeIncremental,
				From:         "zroot/test/a@1",
				To:           "zroot/test/a@2",
				SizeEstimate: 5383936,
			},
		},
		{
			name: "incSendBookmark", in: incSendBookmark,
			exp: &DrySendInfo{
				Type:         DrySendTypeIncremental,
				From:         "zroot/test/a#1",
				To:           "zroot/test/a@2",
				SizeEstimate: 5383312,
			},
		},
		{
			name: "incNoToken", in: incNoToken,
			//exp: &DrySendInfo{
			//	Type:         DrySendTypeIncremental,
			//	From:         "1", // yes, this is actually correct on ZoL 0.7.11
			//	To:           "zroot/test/a@2",
			//	SizeEstimate: 10511856,
			//},
			expErr: true,
		},
		{
			name: "fullNoToken", in: fullNoToken,
			exp: &DrySendInfo{
				Type: DrySendTypeFull,
				From: "",
				To: "zroot/test/a@3",
				SizeEstimate: 10518512,
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
