package tests

import (
	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/platformtest"
	"github.com/zrepl/zrepl/zfs"
)

type resumeTokenTest struct {
	Msg         string
	Token       string
	ExpectToken *zfs.ResumeToken
	ExpectError error
}

func (rtt *resumeTokenTest) Test(t *platformtest.Context) {

	resumeSendSupported, err := zfs.ResumeSendSupported(t)
	if err != nil {
		t.Errorf("cannot determine whether resume supported: %T %s", err, err)
		t.FailNow()
		return
	}

	res, err := zfs.ParseResumeToken(t, rtt.Token)

	// if decoding is not supported, don't bother with the expectations
	if !resumeSendSupported {
		require.Error(t, err)
		require.Equal(t, zfs.ResumeTokenDecodingNotSupported, err)
		return
	}

	if rtt.ExpectError != nil {
		require.EqualValues(t, rtt.ExpectError, err)
		return
	}
	if rtt.ExpectToken != nil {
		require.Nil(t, err)
		require.EqualValues(t, rtt.ExpectToken, res)
		return
	}
}

func ResumeTokenParsing(ctx *platformtest.Context) {

	// cases generated using resumeTokensGenerate.bash on ZoL 0.8.1
	cases := []resumeTokenTest{
		{
			Msg:   "zreplplatformtest/dst/full",
			Token: "1-b338b54f3-c0-789c636064000310a500c4ec50360710e72765a52697303030419460caa7a515a796806474e0f26c48f2499525a9c540ba42430fabfe92fcf4d2cc140686c88a76d578ae45530c90e439c1f27989b9a90c0c5545a905390539892569f945b940234bf48b8b921d12c1660200c61a1aba",
			ExpectToken: &zfs.ResumeToken{
				HasToGUID: true,
				ToGUID:    0x94a20a5f25877859,
				ToName:    "zreplplatformtest/src@a",
			},
		},
		{
			Msg:   "zreplplatformtest/dst/full_raw",
			Token: "1-e3f40c323-f8-789c636064000310a500c4ec50360710e72765a52697303030419460caa7a515a796806474e0f26c48f2499525a9c540da454f0fabfe92fcf4d2cc140686c88a76d578ae45530c90e439c1f27989b9a90c0c5545a905390539892569f945b940234bf48b8b921d12c1e6713320dc9f9c9f5b50945a5c9c9f0d119380ba07265f94580e936200004ff12141",
			ExpectToken: &zfs.ResumeToken{
				HasToGUID:     true,
				ToGUID:        0x94a20a5f25877859,
				ToName:        "zreplplatformtest/src@a",
				HasCompressOK: true, CompressOK: true,
				HasRawOk: true, RawOK: true,
			},
		},

		{
			Msg:   "zreplplatformtest/dst/inc",
			Token: "1-eadabb296-e8-789c636064000310a501c49c50360710a715e5e7a69766a63040416445bb6a3cd7a2290a40363b92bafca4acd4e412060626a83a0cf9b4b4e2d412908c0e5c9e0d493ea9b224b518483ba8ea61d55f920f714515bf9b3fc3c396ef0648f29c60f9bcc4dc54a07c516a414e414e62495a7e512ed0c812fde2a2648724b09900d43e2191",
			ExpectToken: &zfs.ResumeToken{
				HasFromGUID: true, FromGUID: 0x94a20a5f25877859,
				HasToGUID: true, ToGUID: 0xf784e1004f460f7a,
				ToName: "zreplplatformtest/src@b",
			},
		},
		{
			Msg:   "zreplplatformtest/dst/inc_raw",
			Token: "1-1164f8d409-120-789c636064000310a501c49c50360710a715e5e7a69766a63040416445bb6a3cd7a2290a40363b92bafca4acd4e412060626a83a0cf9b4b4e2d412908c0e5c9e0d493ea9b224b51848f368eb61d55f920f714515bf9b3fc3c396ef0648f29c60f9bcc4dc54a07c516a414e414e62495a7e512ed0c812fde2a2648724b079dc0c087f26e7e71614a51617e76743c424a0ee81c9172596c3a41800dd2c2818",
			ExpectToken: &zfs.ResumeToken{
				HasFromGUID: true, FromGUID: 0x94a20a5f25877859,
				HasToGUID: true, ToGUID: 0xf784e1004f460f7a,
				ToName:        "zreplplatformtest/src@b",
				HasCompressOK: true, CompressOK: true,
				HasRawOk: true, RawOK: true,
			},
		},

		// manual test csaes
		{
			Msg:         "corrupted",
			Token:       "1-1164f8d409-120-badf00d064000310a501c49c50360710a715e5e7a69766a63040416445bb6a3cd7a2290a40363b92bafca4acd4e412060626a83a0cf9b4b4e2d412908c0e5c9e0d493ea9b224b51848f368eb61d55f920f714515bf9b3fc3c396ef0648f29c60f9bcc4dc54a07c516a414e414e62495a7e512ed0c812fde2a2648724b079dc0c087f26e7e71614a51617e76743c424a0ee81c9172596c3a41800dd2c2818",
			ExpectError: zfs.ResumeTokenCorruptError,
		},
	}

	for _, test := range cases {
		ctx.Logf("BEGIN SUBTEST: %s", test.Msg)
		test.Test(ctx)
		ctx.Logf("COMPLETE SUBTEST: %s", test.Msg)
	}
}
