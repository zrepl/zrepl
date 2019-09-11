package logic

import (
	"fmt"

	"github.com/zrepl/zrepl/replication/logic/pdu"
)

type tri int

const (
	DontCare = 0x0
	False    = 0x1
	True     = 0x2
)

func (t tri) String() string {
	switch t {
	case DontCare:
		return "dontcare"
	case False:
		return "false"
	case True:
		return "true"
	}
	panic(fmt.Sprintf("unknown variant %v", int(t)))
}

func (t tri) ToPDU() pdu.Tri {
	switch t {
	case DontCare:
		return pdu.Tri_DontCare
	case False:
		return pdu.Tri_False
	case True:
		return pdu.Tri_True
	}
	panic(fmt.Sprintf("unknown variant %v", int(t)))
}

func TriFromBool(b bool) tri {
	if b {
		return True
	}
	return False
}
