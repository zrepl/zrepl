// +build tools

package main

import (
	_ "golang.org/x/tools/cmd/stringer"
	_ "github.com/golang/protobuf/protoc-gen-go"
	_ "github.com/alvaroloes/enumer"
	_ "golang.org/x/tools/cmd/goimports"
	_ "github.com/golangci/golangci-lint/cmd/golangci-lint"
)
