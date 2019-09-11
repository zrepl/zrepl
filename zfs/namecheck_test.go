package zfs

import (
	"strings"
	"testing"
)

func TestEntityNamecheck(t *testing.T) {

	type testcase struct {
		input      string
		entityType EntityType
		ok         bool
	}

	tcs := []testcase{
		{"/", EntityTypeFilesystem, false},
		{"/foo", EntityTypeFilesystem, false},
		{"/foo@bar", EntityTypeSnapshot, false},
		{"foo", EntityTypeBookmark, false},
		{"foo", EntityTypeSnapshot, false},
		{"foo@bar", EntityTypeBookmark, false},
		{"foo#bar", EntityTypeSnapshot, false},
		{"foo#book", EntityTypeBookmark, true},
		{"foo#book@bar", EntityTypeBookmark, false},
		{"foo/book@bar", EntityTypeSnapshot, true},
		{"foo/book#bar", EntityTypeBookmark, true},
		{"foo/for%idden", EntityTypeFilesystem, false},
		{"foo/bår", EntityTypeFilesystem, false},
		{"", EntityTypeFilesystem, false},
		{"foo/bar@", EntityTypeSnapshot, false},
		{"foo/bar#", EntityTypeBookmark, false},
		{"foo/bar#@blah", EntityTypeBookmark, false},
		{"foo bar/baz bar@blah foo", EntityTypeSnapshot, true},
		{"foo bar/baz bar@#lah foo", EntityTypeSnapshot, false},
		{"foo bar/baz bar@@lah foo", EntityTypeSnapshot, false},
		{"foo bar/baz bar##lah foo", EntityTypeBookmark, false},
		{"foo bar/baz@blah/foo", EntityTypeSnapshot, false},
		{"foo bar/baz@blah/foo", EntityTypeFilesystem, false},
		{"foo/b\tr@ba\tz", EntityTypeSnapshot, false},
		{"foo/b\tr@baz", EntityTypeSnapshot, false},
		{"foo/bar@ba\tz", EntityTypeSnapshot, false},
		{"foo/./bar", EntityTypeFilesystem, false},
		{"foo/../bar", EntityTypeFilesystem, false},
		{"foo/bar@..", EntityTypeFilesystem, false},
		{"foo/bar@.", EntityTypeFilesystem, false},
		{strings.Repeat("a", MaxDatasetNameLen), EntityTypeFilesystem, true},
		{strings.Repeat("a", MaxDatasetNameLen) + "a", EntityTypeFilesystem, false},
		{strings.Repeat("a", MaxDatasetNameLen-2) + "/a", EntityTypeFilesystem, true},
		{strings.Repeat("a", MaxDatasetNameLen-4) + "/a@b", EntityTypeSnapshot, true},
		{strings.Repeat("a", MaxDatasetNameLen) + "/a@b", EntityTypeSnapshot, false},
	}

	for idx := range tcs {
		t.Run(tcs[idx].input, func(t *testing.T) {
			tc := tcs[idx]
			err := EntityNamecheck(tc.input, tc.entityType)
			if !((err == nil && tc.ok) || (err != nil && !tc.ok)) {
				t.Errorf("expecting ok=%v but got err=%v", tc.ok, err)
			}
		})
	}

}
