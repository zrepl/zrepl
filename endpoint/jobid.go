package endpoint

import (
	"encoding/json"
	"fmt"

	"github.com/pkg/errors"

	"github.com/zrepl/zrepl/zfs"
)

// JobID instances returned by MakeJobID() guarantee JobID.String() to
// return a string that can be used in a ZFS dataset name and hold tag.
type JobID struct {
	jid string
}

func MakeJobID(s string) (JobID, error) {
	if len(s) == 0 {
		return JobID{}, fmt.Errorf("must not be empty string")
	}

	if err := zfs.ComponentNamecheck(s); err != nil {
		return JobID{}, errors.Wrap(err, "must be usable as a dataset path component")
	}

	if _, err := stepBookmarkNameImpl("pool/ds", 0xface601d, s); err != nil {
		// note that this might still fail due to total maximum name length, but we can't enforce that
		return JobID{}, errors.Wrap(err, "must be usable for a step bookmark")
	}

	if _, err := stepHoldTagImpl(s); err != nil {
		return JobID{}, errors.Wrap(err, "must be usable for a step hold tag")
	}

	if _, err := lastReceivedHoldImpl(s); err != nil {
		return JobID{}, errors.Wrap(err, "must be usable as a last-received-hold tag")
	}

	// FIXME replication cursor bookmark name

	_, err := zfs.NewDatasetPath(s)
	if err != nil {
		return JobID{}, fmt.Errorf("must be usable in a ZFS dataset path: %s", err)
	}

	return JobID{s}, nil
}

func MustMakeJobID(s string) JobID {
	jid, err := MakeJobID(s)
	if err != nil {
		panic(err)
	}
	return jid
}

func (j JobID) expectInitialized() {
	if j.jid == "" {
		panic("use of unitialized JobID")
	}
}

func (j JobID) String() string {
	j.expectInitialized()
	return j.jid
}

var _ json.Marshaler = JobID{}
var _ json.Unmarshaler = (*JobID)(nil)

func (j JobID) MarshalJSON() ([]byte, error) { return json.Marshal(j.jid) }

func (j *JobID) UnmarshalJSON(b []byte) error {
	return json.Unmarshal(b, &j.jid)
}

func (j JobID) MustValidate() { j.expectInitialized() }
