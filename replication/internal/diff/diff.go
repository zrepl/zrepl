package mainfsm

import (
	"sort"

	. "github.com/zrepl/zrepl/replication/pdu"
)

type ConflictNoCommonAncestor struct {
	SortedSenderVersions, SortedReceiverVersions []*FilesystemVersion
}

func (c *ConflictNoCommonAncestor) Error() string {
	return "no common snapshot or suitable bookmark between sender and receiver"
}

type ConflictDiverged struct {
	SortedSenderVersions, SortedReceiverVersions []*FilesystemVersion
	CommonAncestor                               *FilesystemVersion
	SenderOnly, ReceiverOnly                     []*FilesystemVersion
}

func (c *ConflictDiverged) Error() string {
	return "the receiver's latest snapshot is not present on sender"
}

func SortVersionListByCreateTXGThenBookmarkLTSnapshot(fsvslice []*FilesystemVersion) []*FilesystemVersion {
	lesser := func(s []*FilesystemVersion) func(i, j int) bool {
		return func(i, j int) bool {
			if s[i].CreateTXG < s[j].CreateTXG {
				return true
			}
			if s[i].CreateTXG == s[j].CreateTXG {
				//  Bookmark < Snapshot
				return s[i].Type == FilesystemVersion_Bookmark && s[j].Type == FilesystemVersion_Snapshot
			}
			return false
		}
	}
	if sort.SliceIsSorted(fsvslice, lesser(fsvslice)) {
		return fsvslice
	}
	sorted := make([]*FilesystemVersion, len(fsvslice))
	copy(sorted, fsvslice)
	sort.Slice(sorted, lesser(sorted))
	return sorted
}

// conflict may be a *ConflictDiverged or a *ConflictNoCommonAncestor
func IncrementalPath(receiver, sender []*FilesystemVersion) (incPath []*FilesystemVersion, conflict error) {

	receiver = SortVersionListByCreateTXGThenBookmarkLTSnapshot(receiver)
	sender = SortVersionListByCreateTXGThenBookmarkLTSnapshot(sender)

	// Find most recent common ancestor by name, preferring snapshots over bookmarks

	mrcaRcv := len(receiver) - 1
	mrcaSnd := len(sender) - 1

	for mrcaRcv >= 0 && mrcaSnd >= 0 {
		if receiver[mrcaRcv].Guid == sender[mrcaSnd].Guid {
			if mrcaSnd-1 >= 0 && sender[mrcaSnd-1].Guid == sender[mrcaSnd].Guid && sender[mrcaSnd-1].Type == FilesystemVersion_Bookmark {
				// prefer bookmarks over snapshots as the snapshot might go away sooner
				mrcaSnd -= 1
			}
			break
		}
		if receiver[mrcaRcv].CreateTXG < sender[mrcaSnd].CreateTXG {
			mrcaSnd--
		} else {
			mrcaRcv--
		}
	}

	if mrcaRcv == -1 || mrcaSnd == -1 {
		return nil, &ConflictNoCommonAncestor{
			SortedSenderVersions:   sender,
			SortedReceiverVersions: receiver,
		}
	}

	if mrcaRcv != len(receiver)-1 {
		return nil, &ConflictDiverged{
			SortedSenderVersions:   sender,
			SortedReceiverVersions: receiver,
			CommonAncestor:         sender[mrcaSnd],
			SenderOnly:             sender[mrcaSnd+1:],
			ReceiverOnly:           receiver[mrcaRcv+1:],
		}
	}

	// incPath must not contain bookmarks except initial one,
	incPath = make([]*FilesystemVersion, 0, len(sender))
	incPath = append(incPath, sender[mrcaSnd])
	// it's ok if incPath[0] is a bookmark, but not the subsequent ones in the incPath
	for i := mrcaSnd + 1; i < len(sender); i++ {
		if sender[i].Type == FilesystemVersion_Snapshot && incPath[len(incPath)-1].Guid != sender[i].Guid {
			incPath = append(incPath, sender[i])
		}
	}
	if len(incPath) == 1 {
		// nothing to do
		incPath = incPath[1:]
	}
	return incPath, nil
}
