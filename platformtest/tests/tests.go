package tests

import (
	"reflect"
	"runtime"

	"github.com/zrepl/zrepl/platformtest"
)

type Case func(*platformtest.Context)

func (c Case) String() string {
	return runtime.FuncForPC(reflect.ValueOf(c).Pointer()).Name()
}

//go:generate ../../artifacts/generate-platform-test-list github.com/zrepl/zrepl/platformtest/tests
