package filters

import (
	"testing"

	"github.com/zrepl/zrepl/zfs"
)

func TestDatasetMapFilter(t *testing.T) {

	type testCase struct {
		name      string
		filter    map[string]string
		checkPass map[string]bool // each entry is checked to match the filter's `pass` return value
	}

	tcs := []testCase{
		{
			"default_no_match",
			map[string]string{},
			map[string]bool{
				"":      false,
				"foo":   false,
				"zroot": false,
			},
		},
		{
			"more_specific_path_has_precedence",
			map[string]string{
				"tank<":         "ok",
				"tank/tmp<":     "!",
				"tank/home/x<":  "!",
				"tank/home/x/1": "ok",
			},
			map[string]bool{
				"zroot":         false,
				"tank":          true,
				"tank/tmp":      false,
				"tank/tmp/foo":  false,
				"tank/home/x":   false,
				"tank/home/y":   true,
				"tank/home/x/1": true,
				"tank/home/x/2": false,
			},
		},
		{
			"precedence_of_specific_over_subtree_wildcard_on_same_path",
			map[string]string{
				"tank/home/bob":  "ok",
				"tank/home/bob<": "!",
			},
			map[string]bool{
				"tank/home/bob":           true,
				"tank/home/bob/downloads": false,
			},
		},
	}

	for tc := range tcs {
		t.Run(tcs[tc].name, func(t *testing.T) {
			c := tcs[tc]
			f := NewDatasetMapFilter(len(c.filter), true)
			for p, a := range c.filter {
				err := f.Add(p, a)
				if err != nil {
					t.Fatalf("incorrect filter spec: %s", err)
				}
			}
			for p, checkPass := range c.checkPass {
				zp, err := zfs.NewDatasetPath(p)
				if err != nil {
					t.Fatalf("incorrect path spec: %s", err)
				}
				pass, err := f.Filter(zp)
				if err != nil {
					t.Fatalf("unexpected filter error: %s", err)
				}
				ok := pass == checkPass
				failstr := "OK"
				if !ok {
					failstr = "FAIL"
					t.Fail()
				}
				t.Logf("%-40q  %5v  (exp=%v act=%v)", p, failstr, checkPass, pass)
			}
		})
	}

}
