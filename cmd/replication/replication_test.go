package replication_test

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/zrepl/zrepl/cmd/replication"
	"github.com/zrepl/zrepl/zfs"
	"io"
	"testing"
)

type IncrementalPathSequenceStep struct {
	SendRequest    replication.SendRequest
	SendResponse   replication.SendResponse
	SendError      error
	ReceiveRequest replication.ReceiveRequest
	ReceiveWriter  io.Writer
	ReceiveError   error
}

type MockIncrementalPathRecorder struct {
	T        *testing.T
	Sequence []IncrementalPathSequenceStep
	Pos      int
}

func (m *MockIncrementalPathRecorder) Receive(r replication.ReceiveRequest) (io.Writer, error) {
	if m.Pos >= len(m.Sequence) {
		m.T.Fatal("unexpected Receive")
	}
	i := m.Sequence[m.Pos]
	m.Pos++
	if !assert.Equal(m.T, i.ReceiveRequest, r) {
		m.T.FailNow()
	}
	return i.ReceiveWriter, i.ReceiveError
}

func (m *MockIncrementalPathRecorder) Send(r replication.SendRequest) (replication.SendResponse, error) {
	if m.Pos >= len(m.Sequence) {
		m.T.Fatal("unexpected Send")
	}
	i := m.Sequence[m.Pos]
	m.Pos++
	if !assert.Equal(m.T, i.SendRequest, r) {
		m.T.FailNow()
	}
	return i.SendResponse, i.SendError
}

func (m *MockIncrementalPathRecorder) Finished() bool {
	return m.Pos == len(m.Sequence)
}

type DiscardCopier struct{}

func (DiscardCopier) Copy(writer io.Writer, reader io.Reader) (int64, error) {
	return 0, nil
}

type IncrementalPathReplicatorTest struct {
	Msg        string
	Filesystem replication.Filesystem
	Path       []zfs.FilesystemVersion
	Steps      []IncrementalPathSequenceStep
}

func (test *IncrementalPathReplicatorTest) Test(t *testing.T) {

	t.Log(test.Msg)

	rec := &MockIncrementalPathRecorder{
		T:        t,
		Sequence: test.Steps,
	}

	ipr := replication.NewIncrementalPathReplicator()
	ipr.Replicate(
		context.TODO(),
		rec,
		rec,
		DiscardCopier{},
		test.Filesystem,
		test.Path,
	)

	assert.True(t, rec.Finished())

}

func TestIncrementalPathReplicator_Replicate(t *testing.T) {

	tbl := []IncrementalPathReplicatorTest{
		{
			Msg: "generic happy place with resume token",
			Filesystem: replication.Filesystem{
				Path:        "foo/bar",
				ResumeToken: "blafoo",
			},
			Path: fsvlist("@a,1", "@b,2", "@c,3"),
			Steps: []IncrementalPathSequenceStep{
				{
					SendRequest: replication.SendRequest{
						Filesystem:  "foo/bar",
						From:        "@a,1",
						To:          "@b,2",
						ResumeToken: "blafoo",
					},
				},
				{
					ReceiveRequest: replication.ReceiveRequest{
						Filesystem:  "foo/bar",
						ResumeToken: "blafoo",
					},
				},
				{
					SendRequest: replication.SendRequest{
						Filesystem: "foo/bar",
						From:       "@b,2",
						To:         "@c,3",
					},
				},
				{
					ReceiveRequest: replication.ReceiveRequest{
						Filesystem: "foo/bar",
					},
				},
			},
		},
		{
			Msg: "no action on empty sequence",
			Filesystem: replication.Filesystem{
				Path: "foo/bar",
			},
			Path:  fsvlist(),
			Steps: []IncrementalPathSequenceStep{},
		},
		{
			Msg: "no action on invalid path",
			Filesystem: replication.Filesystem{
				Path: "foo/bar",
			},
			Path:  fsvlist("@justone,1"),
			Steps: []IncrementalPathSequenceStep{},
		},
	}

	for _, test := range tbl {
		test.Test(t)
	}

}
