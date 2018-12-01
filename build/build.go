//+build windows

// This package cannot actually be built, and since the Travis
// tests run go test ./..., we have to avoid a build attempt.
// Windows is not supported atm, so this works ¯\_(ツ)_/¯

// This package is a pseudo-user of build-time dependencies for zrepl,
// mostly for various code-generation tools.
//
// The imports are necessary to satisfy go dep
package main

import (
	_ "fmt"
	_ "github.com/alvaroloes/enumer"
	_ "github.com/golang/protobuf/protoc-gen-go"
	_ "golang.org/x/tools/cmd/stringer"
)

func main() {
	fmt.Println("just a placeholder")
}
