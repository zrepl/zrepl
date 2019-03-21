package client

import "testing"

func TestMigrationsUnambiguousNames(t *testing.T) {
	names := make(map[string]bool)
	for _, mig := range migrations {
		if _, ok := names[mig.Use]; ok {
			t.Errorf("duplicate migration name %q", mig.Use)
			t.FailNow()
			return
		} else {
			names[mig.Use] = true
		}
	}
}
