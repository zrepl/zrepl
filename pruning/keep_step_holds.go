package pruning

import (
	"github.com/pkg/errors"
	"github.com/zrepl/zrepl/endpoint"
)

type KeepStepHolds struct {
	keepJobIDs map[endpoint.JobID]bool
}

var _ KeepRule = (*KeepStepHolds)(nil)

func NewKeepStepHolds(mainJobId endpoint.JobID, additionalJobIdsStrings []string) (_ *KeepStepHolds, err error) {
	additionalJobIds := make(map[endpoint.JobID]bool, len(additionalJobIdsStrings))

	mainJobId.MustValidate()
	additionalJobIds[mainJobId] = true

	for i := range additionalJobIdsStrings {
		ajid, err := endpoint.MakeJobID(additionalJobIdsStrings[i])
		if err != nil {
			return nil, errors.WithMessagef(err, "cannot parse job id %q: %s", additionalJobIdsStrings[i])
		}
		if additionalJobIds[ajid] == true {
			return nil, errors.Errorf("duplicate job id %q", ajid)
		}
	}
	return &KeepStepHolds{additionalJobIds}, nil
}

func (h *KeepStepHolds) KeepRule(snaps []Snapshot) PruneSnapshotsResult {
	return partitionSnapList(snaps, func(s Snapshot) bool {
		holdingJobIDs := make(map[endpoint.JobID]bool)
		for _, h := range s.StepHolds() {
			holdingJobIDs[h.GetJobID()] = true
		}
		oneOrMoreOfOurJobIDsHoldsSnap := false
		for kjid := range h.keepJobIDs {
			oneOrMoreOfOurJobIDsHoldsSnap = oneOrMoreOfOurJobIDsHoldsSnap || holdingJobIDs[kjid]
		}
		return !oneOrMoreOfOurJobIDsHoldsSnap
	})
}
