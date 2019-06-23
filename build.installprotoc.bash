#!/usr/bin/env bash
set -euo pipefail
set -x

MACH=$(uname -m)
MACH="${MACH/aarch64/aarch_64}"

VERSION=3.6.1
FILENAME=protoc-"$VERSION"-linux-"$MACH".zip

if [ -e "$FILENAME" ]; then
    echo "$FILENAME" already exists 1>&2
    exit 1
fi

wget https://github.com/protocolbuffers/protobuf/releases/download/v"$VERSION"/"$FILENAME"

stat "$FILENAME"

sha256sum -c --ignore-missing <<EOF
6003de742ea3fcf703cfec1cd4a3380fd143081a2eb0e559065563496af27807  protoc-3.6.1-linux-x86_64.zip
af8e5aaaf39ddec62ec8dd2be1b8d9602c6da66564883a16393ade5f71170922  protoc-3.6.1-linux-aarch_64.zip
EOF

unzip -d /usr "$FILENAME"