#!/usr/bin/env bash
set -euo pipefail
set -x

cd "$1"

MACH=$(uname -m)
MACH="${MACH/aarch64/aarch_64}"

VERSION=28.0
FILENAME=protoc-"$VERSION"-linux-"$MACH".zip

if [ -e "$FILENAME" ]; then
    echo "$FILENAME" already exists 1>&2
    exit 1
fi

wget --continue https://github.com/protocolbuffers/protobuf/releases/download/v"$VERSION"/"$FILENAME"

stat "$FILENAME"

sha256sum -c --ignore-missing <<EOF
d622619dcbfb5ecb281cfb92c1a74d6a0f42e752d9a2774b197f475f7ab1c8c4  protoc-28.0-linux-aarch_64.zip
b2e187c8b9f2d97cd3ecae4926d1bb2cbebe3ab768e7c987cbc86bb17f319358  protoc-28.0-linux-x86_64.zip
EOF

unzip -d . "$FILENAME"
