#!/usr/bin/env bash
set -euo pipefail
set -x

cd "$1"

MACH=$(uname -m)
MACH="${MACH/aarch64/aarch_64}"

VERSION=33.4
FILENAME=protoc-"$VERSION"-linux-"$MACH".zip

if [ -e "$FILENAME" ]; then
    echo "$FILENAME" already exists 1>&2
    exit 1
fi

wget --continue https://github.com/protocolbuffers/protobuf/releases/download/v"$VERSION"/"$FILENAME"

stat "$FILENAME"

# Select the correct checksum for the downloaded architecture
case "$MACH" in
    aarch_64) EXPECTED_SHA256="15aa988f4a6090636525ec236a8e4b3aab41eef402751bd5bb2df6afd9b7b5a5" ;;
    x86_64)   EXPECTED_SHA256="c0040ea9aef08fdeb2c74ca609b18d5fdbfc44ea0042fcfbfb38860d35f7dd66" ;;
    *)        echo "Unknown architecture: $MACH" >&2; exit 1 ;;
esac

# Verify checksum explicitly - fails if hash doesn't match
echo "$EXPECTED_SHA256  $FILENAME" | sha256sum -c -

unzip -d . "$FILENAME"
