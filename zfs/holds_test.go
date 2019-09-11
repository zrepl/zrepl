package zfs

import (
	"testing"

	"github.com/kr/pretty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoZFSReleaseAllOlderAndIncOrExcludingGUIDFindSnapshots(t *testing.T) {

	// what we test here: sort bookmark #3 before @3
	// => assert that the function doesn't stop at the first guid match
	//    (which might be a bookmark, depending on zfs list ordering)
	//    but instead considers the entire stride of boomarks and snapshots with that guid
	//
	// also, throw in unordered createtxg for good measure
	list, err := doZFSReleaseAllOlderAndIncOrExcludingGUIDParseListOutput(
		[]byte("snapshot\tfoo@1\t1\t1013001\t1\n" +
			"snapshot\tfoo@2\t2\t2013002\t1\n" +
			"bookmark\tfoo#3\t3\t7013003\t-\n" +
			"snapshot\tfoo@6\t6\t5013006\t1\n" +
			"snapshot\tfoo@3\t3\t7013003\t1\n" +
			"snapshot\tfoo@4\t3\t6013004\t1\n" +
			""),
	)
	require.NoError(t, err)
	t.Log(pretty.Sprint(list))
	require.Equal(t, 6, len(list))
	require.Equal(t, EntityTypeBookmark, list[2].entityType)

	releaseSnaps, err := doZFSReleaseAllOlderAndIncOrExcludingGUIDFindSnapshots(7013003, true, list)
	t.Logf("releasedSnaps = %#v", releaseSnaps)
	assert.NoError(t, err)

	assert.Equal(t, []string{"foo@1", "foo@2", "foo@3"}, releaseSnaps)
}
