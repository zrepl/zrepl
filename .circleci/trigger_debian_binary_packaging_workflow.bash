#!/usr/bin/env bash
set -euo pipefail

COMMIT="$1"
GO_VERSION="$2"

curl -v -X POST https://api.github.com/repos/zrepl/debian-binary-packaging/dispatches \
    -H 'Accept: application/vnd.github.v3+json' \
    -H "Authorization: token $GITHUB_ACCESS_TOKEN" \
    --data '{"event_type": "push", "client_payload": { "zrepl_main_repo_commit": "'"$COMMIT"'", "go_version": "'"$GO_VERSION"'" }}'
