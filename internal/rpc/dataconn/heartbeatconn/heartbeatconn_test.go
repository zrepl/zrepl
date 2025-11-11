package heartbeatconn

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/LyingCak3/zrepl/internal/rpc/dataconn/frameconn"
)

func TestFrameTypes(t *testing.T) {
	assert.True(t, frameconn.IsPublicFrameType(heartbeat))
}

func TestNegativeTimer(t *testing.T) {

	timer := time.NewTimer(-1 * time.Second)
	defer timer.Stop()
	time.Sleep(100 * time.Millisecond)
	select {
	case <-timer.C:
		t.Log("timer with negative time fired, that's what we want")
	default:
		t.Fail()
	}
}
