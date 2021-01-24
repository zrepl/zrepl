// +build tools

package main

// the lines are parsed by lazy.sh, do not edit
import (
	_ "github.com/alvaroloes/enumer"
	_ "github.com/golangci/golangci-lint/cmd/golangci-lint"
	_ "github.com/wadey/gocovmerge"
	_ "golang.org/x/tools/cmd/goimports"
	_ "golang.org/x/tools/cmd/stringer"
	_ "google.golang.org/grpc/cmd/protoc-gen-go-grpc"
	_ "google.golang.org/protobuf/cmd/protoc-gen-go"
)
