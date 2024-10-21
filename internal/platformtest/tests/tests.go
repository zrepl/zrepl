package tests

import (
	"reflect"
	"runtime"

	"github.com/zrepl/zrepl/internal/platformtest"
)

type Case func(*platformtest.Context)

func (c Case) String() string {
	return runtime.FuncForPC(reflect.ValueOf(c).Pointer()).Name()
}

//go:generate go run ./gen ./
