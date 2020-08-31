package job

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zrepl/zrepl/endpoint"
	"github.com/zrepl/zrepl/transport"
)

func TestFakeActiveSideDirectMethodInvocationClientIdentityDoesNotPassValidityTest(t *testing.T) {
	jobid, err := endpoint.MakeJobID("validjobname")
	require.NoError(t, err)
	clientIdentity := FakeActiveSideDirectMethodInvocationClientIdentity(jobid)
	t.Logf("%v", clientIdentity)
	err = transport.ValidateClientIdentity(clientIdentity)
	assert.Error(t, err)
}
