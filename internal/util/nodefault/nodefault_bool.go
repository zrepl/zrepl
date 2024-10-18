package nodefault

import "fmt"

type Bool struct{ B bool }

func (n *Bool) ValidateNoDefault() error {
	if n == nil {
		return fmt.Errorf("must explicitly set `true` or `false`")
	}
	return nil
}

func (n *Bool) String() string {
	if n == nil {
		return "unset"
	}
	return fmt.Sprintf("%v", n.B)
}
