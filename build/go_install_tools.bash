#!/usr/bin/env bash

set -euo pipefail
set -x

OUTDIR="$(readlink -f "$1")"
if [ -e "$OUTDIR" ]; then
    echo "$OUTDIR" already exists 1>&2
    exit 1
fi

# go install command below will install tools to $GOBIN
export GOBIN="$OUTDIR"

cd "$(dirname "$0")"

export GO111MODULE=on # otherwise, a checkout of this repo in GOPATH will disable modules on Go 1.12 and earlier
source <(go env)
# build tools for the host platform
export GOOS="$GOHOSTOS"
export GOARCH="$GOHOSTARCH"
# TODO GOARM=$GOHOSTARM?

cat tools.go | grep _ | awk -F'"' '{print $2}' | tee | xargs -tI '{}' go install '{}'
