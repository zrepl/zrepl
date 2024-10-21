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

source ./go_install_host_tool.source

cat tools.go | grep _ | awk -F'"' '{print $2}' | tee | xargs -tI '{}' go install '{}'
