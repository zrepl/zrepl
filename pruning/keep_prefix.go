package pruning

import (
	"regexp"
)

type KeepRegex struct {
	expr *regexp.Regexp
}

var _ KeepRule = &KeepRegex{}

func NewKeepRegex(expr string) (*KeepRegex, error) {
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, err
	}
	return &KeepRegex{re}, nil
}

func MustKeepRegex(expr string) *KeepRegex {
	k, err := NewKeepRegex(expr)
	if err != nil {
		panic(err)
	}
	return k
}

func (k *KeepRegex) KeepRule(snaps []Snapshot) []Snapshot {
	return filterSnapList(snaps, func(s Snapshot) bool {
		return k.expr.FindStringIndex(s.Name()) == nil
	})
}
