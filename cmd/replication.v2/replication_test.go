package replication_test

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/zrepl/zrepl/cmd/replication"
	"io"
	"testing"
)

type IncrementalPathSequenceStep struct {
	SendRequest    *replication.SendReq
	SendResponse   *replication.SendRes
	SendReader io.ReadCloser
	SendError      error
	ReceiveRequest *replication.ReceiveReq
	ReceiveError   error
}

type MockIncrementalPathRecorder struct {
	T        *testing.T
	Sequence []IncrementalPathSequenceStep
	Pos      int
}

func (m *MockIncrementalPathRecorder) Receive(ctx context.Context, r *replication.ReceiveReq, rs io.ReadCloser) (error) {
	if m.Pos >= len(m.Sequence) {
		m.T.Fatal("unexpected Receive")
	}
	i := m.Sequence[m.Pos]
	m.Pos++
	if !assert.Equal(m.T, i.ReceiveRequest, r) {
		m.T.FailNow()
	}
	return i.ReceiveError
}

func (m *MockIncrementalPathRecorder) Send(ctx context.Context, r *replication.SendReq) (*replication.SendRes, io.ReadCloser, error) {
	if m.Pos >= len(m.Sequence) {
		m.T.Fatal("unexpected Send")
	}
	i := m.Sequence[m.Pos]
	m.Pos++
	if !assert.Equal(m.T, i.SendRequest, r) {
		m.T.FailNow()
	}
	return i.SendResponse, i.SendReader, i.SendError
}

func (m *MockIncrementalPathRecorder) Finished() bool {
	return m.Pos == len(m.Sequence)
}

//type IncrementalPathReplicatorTest struct {
//	Msg        string
//	Filesystem *replication.Filesystem
//	Path       []*replication.FilesystemVersion
//	Steps      []IncrementalPathSequenceStep
//}
//
//func (test *IncrementalPathReplicatorTest) Test(t *testing.T) {
//
//	t.Log(test.Msg)
//
//	rec := &MockIncrementalPathRecorder{
//		T:        t,
//		Sequence: test.Steps,
//	}
//
//	ctx := replication.ContextWithLogger(context.Background(), testLog{t})
//
//	ipr := replication.NewIncrementalPathReplicator()
//	ipr.Replicate(
//		ctx,
//		rec,
//		rec,
//		DiscardCopier{},
//		test.Filesystem,
//		test.Path,
//	)
//
//	assert.True(t, rec.Finished())
//
//}

//type testLog struct {
//	t *testing.T
//}
//
//var _ replication.Logger = testLog{}
//
//func (t testLog) Infof(fmt string, args ...interface{}) {
//	t.t.Logf(fmt, args)
//}
//func (t testLog) Errorf(fmt string, args ...interface{}) {
//	t.t.Logf(fmt, args)
//}


//func TestIncrementalPathReplicator_Replicate(t *testing.T) {
//
//	tbl := []IncrementalPathReplicatorTest{
//		{
//			Msg: "generic happy place with resume token",
//			Filesystem: &replication.Filesystem{
//				Path:        "foo/bar",
//				ResumeToken: "blafoo",
//			},
//			Path: fsvlist("@a,1", "@b,2", "@c,3"),
//			Steps: []IncrementalPathSequenceStep{
//				{
//					SendRequest: &replication.SendReq{
//						Filesystem:  "foo/bar",
//						From:        "@a,1",
//						To:          "@b,2",
//						ResumeToken: "blafoo",
//					},
//					SendResponse: &replication.SendRes{
//						UsedResumeToken: true,
//					},
//				},
//				{
//					ReceiveRequest: &replication.ReceiveReq{
//						Filesystem:  "foo/bar",
//						ClearResumeToken: false,
//					},
//				},
//				{
//					SendRequest: &replication.SendReq{
//						Filesystem: "foo/bar",
//						From:       "@b,2",
//						To:         "@c,3",
//					},
//				},
//				{
//					ReceiveRequest: &replication.ReceiveReq{
//						Filesystem: "foo/bar",
//					},
//				},
//			},
//		},
//		{
//			Msg: "no action on empty sequence",
//			Filesystem: &replication.Filesystem{
//				Path: "foo/bar",
//			},
//			Path:  fsvlist(),
//			Steps: []IncrementalPathSequenceStep{},
//		},
//		{
//			Msg: "full send on single entry path",
//			Filesystem: &replication.Filesystem{
//				Path: "foo/bar",
//			},
//			Path:  fsvlist("@justone,1"),
//			Steps: []IncrementalPathSequenceStep{
//				{
//					SendRequest: &replication.SendReq{
//						Filesystem: "foo/bar",
//						From: "@justone,1",
//						To: "", // empty means full send
//					},
//					SendResponse: &replication.SendRes{
//						UsedResumeToken: false,
//					},
//				},
//				{
//					ReceiveRequest: &replication.ReceiveReq{
//						Filesystem: "foo/bar",
//						ClearResumeToken: false,
//					},
//				},
//			},
//		},
//	}
//
//	for _, test := range tbl {
//		test.Test(t)
//	}
//
//}
