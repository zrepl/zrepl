#!/usr/bin/env bash
set -euo pipefail
set -x

cd "$1"

MACH=$(uname -m)
MACH="${MACH/aarch64/aarch_64}"

VERSION=33.5
FILENAME=protoc-"$VERSION"-linux-"$MACH".zip

if [ -e "$FILENAME" ]; then
    echo "$FILENAME" already exists 1>&2
    exit 1
fi

wget --continue https://github.com/protocolbuffers/protobuf/releases/download/v"$VERSION"/"$FILENAME"

stat "$FILENAME"

# Select the correct checksum for the downloaded architecture
case "$MACH" in
    aarch_64) EXPECTED_SHA256="2b0fcf9b2c32cbadccc0eb7a88b841fffecd4a06fc80acdba2b5be45e815c38a" ;;
    x86_64)   EXPECTED_SHA256="24e58fb231d50306ee28491f33a170301e99540f7e29ca461e0e80fd1239f8d1" ;;
    *)        echo "Unknown architecture: $MACH" >&2; exit 1 ;;
esac

# Verify checksum explicitly - fails if hash doesn't match
echo "$EXPECTED_SHA256  $FILENAME" | sha256sum -c -

unzip -d . "$FILENAME"
