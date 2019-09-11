package endpoint

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseJobAndGuidBookmarkName(t *testing.T) {

	type Case struct {
		input     string
		expectErr bool

		guid  uint64
		jobid string
	}

	cases := []Case{
		{
			`p1/sync#zrepl_CURSOR_G_932f3a7089080ce2_J_push with legitimate name`,
			false, 0x932f3a7089080ce2, "push with legitimate name",
		},
		{
			input:     `p1/sync#zrepl_CURSOR_G_932f3a7089_J_push with legitimate name`,
			expectErr: true,
		},
		{
			input:     `p1/sync#zrepl_CURSOR_G_932f3a7089080ce2_J_push with il\tlegitimate name`,
			expectErr: true,
		},
		{
			input:     `p1/sync#otherprefix_G_932f3a7089080ce2_J_push with legitimate name`,
			expectErr: true,
		},
	}

	for i := range cases {
		t.Run(cases[i].input, func(t *testing.T) {
			guid, jobid, err := parseJobAndGuidBookmarkName(cases[i].input, replicationCursorBookmarkNamePrefix)
			if cases[i].expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, cases[i].guid, guid)
				assert.Equal(t, MustMakeJobID(cases[i].jobid), jobid)
			}
		})
	}

}
