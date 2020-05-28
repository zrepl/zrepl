package endpoint

import "github.com/zrepl/zrepl/replication/logic/pdu"

func SendAbstractionToPDU(a Abstraction) *pdu.SendAbstraction {
	var ty pdu.SendAbstraction_SendAbstractionType
	switch a.GetType() {
	case AbstractionLastReceivedHold:
		panic(a)
	case AbstractionReplicationCursorBookmarkV1:
		panic(a)
	case AbstractionReplicationCursorBookmarkV2:
		ty = pdu.SendAbstraction_ReplicationCursorV2
	case AbstractionStepHold:
		ty = pdu.SendAbstraction_StepHold
	case AbstractionStepBookmark:
		ty = pdu.SendAbstraction_StepBookmark
	default:
		panic(a)
	}

	version := a.GetFilesystemVersion()
	return &pdu.SendAbstraction{
		Type:    ty,
		JobID:   (*a.GetJobID()).String(),
		Version: pdu.FilesystemVersionFromZFS(&version),
	}
}
