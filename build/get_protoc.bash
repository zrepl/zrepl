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

# Select the correct checksum for the downloaded architecture
case "$MACH" in
    aarch_64) EXPECTED_SHA256="d622619dcbfb5ecb281cfb92c1a74d6a0f42e752d9a2774b197f475f7ab1c8c4" ;;
    x86_64)   EXPECTED_SHA256="b2e187c8b9f2d97cd3ecae4926d1bb2cbebe3ab768e7c987cbc86bb17f319358" ;;
    *)        echo "Unknown architecture: $MACH" >&2; exit 1 ;;
esac

# Verify checksum explicitly - fails if hash doesn't match
echo "$EXPECTED_SHA256  $FILENAME" | sha256sum -c -

unzip -d . "$FILENAME"
