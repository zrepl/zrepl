#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

commit="$1"
build="$2"
src="$3"
dst="$4"

[ -n "$commit" -a -n "$build" -a -n "$src" -a -n "$dst" ] || ( echo "arguments must not be empty"; exit 1 )

aws="aws --endpoint-url https://minio.cschwarz.com --no-sign-request"


path="s3://zrepl-ci-artifacts/$commit/$build/$src"
$aws s3 cp "$path" "$dst"
