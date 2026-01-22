#!/usr/bin/env bash
set -euo pipefail
set -x

# golangci-lint recommends against using `go get` and friends to install it

cd "$1"

VERSION=2.8.0
FILENAME=golangci-lint-"$VERSION"-linux-"$GOHOSTARCH".tar.gz

if [ -e "$FILENAME" ]; then
    echo "$FILENAME" already exists 1>&2
    exit 1
fi

wget --continue https://github.com/golangci/golangci-lint/releases/download/v"$VERSION"/"$FILENAME"

stat "$FILENAME"

# Select the correct checksum for the downloaded architecture
case "$GOHOSTARCH" in
    arm64) EXPECTED_SHA256="2a58388db8af5ab9330791cea0ebdd4100723cd05ad7185d92febaaee272ec9a" ;;
    amd64) EXPECTED_SHA256="7048bc6b25c9515ed092c83f9fa8709ca97937ead52d9ff317a143299ee97a50" ;;
    *)     echo "Unknown architecture: $GOHOSTARCH" >&2; exit 1 ;;
esac

# Verify checksum explicitly - fails if hash doesn't match
echo "$EXPECTED_SHA256  $FILENAME" | sha256sum -c -

tar -x  --strip-components=1 -f "$FILENAME"

stat ./golangci-lint
