#!/usr/bin/env bash
set -euo pipefail
set -x

source ../../../build/go_install_host_tool.source

GOOS=$GOHOSTOS GOARCH=$GOHOSTARCH go run ./gen ./
