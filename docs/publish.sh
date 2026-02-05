#!/bin/bash
set -euo pipefail

GHPAGESREPO="https://github.com/zrepl/zrepl.github.io.git"
SCRIPTDIR=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )
PUBLICDIR="${SCRIPTDIR}/public_git"
ROOTDIR="${SCRIPTDIR}/.."

PUSH=false
DO_CLONE=false
NON_INTERACTIVE=false

while getopts "caPh" arg; do
    case "$arg" in
        "a") NON_INTERACTIVE=true ;;
        "c") DO_CLONE=true ;;
        "P") PUSH=true ;;
        "h") echo "Usage: $0 [-c clone] [-a auto/non-interactive] [-P push]"; exit 0 ;;
        *) echo "invalid option"; exit 1 ;;
    esac
done

cd "$SCRIPTDIR"

# Clone or verify repo
if [ ! -d "$PUBLICDIR" ]; then
    if $DO_CLONE; then
        git clone "${GHPAGESREPO}" "${PUBLICDIR}"
    else
        echo "Run with -c to clone ${GHPAGESREPO}"
        exit 1
    fi
fi

if ! $NON_INTERACTIVE; then
    echo -n "PRESS ENTER to confirm you committed and pushed docs changes to the zrepl repo"
    read -r
fi

# Verify we're in the right repo
cd "$PUBLICDIR"
if ! git remote get-url origin | grep -qE "^${GHPAGESREPO}\$"; then
    echo "ERROR: ${PUBLICDIR} is not a clone of ${GHPAGESREPO}"
    exit 1
fi

# Reset public repo to latest
echo "Resetting GitHub pages repo to latest commit..."
git fetch origin
git reset --hard origin/master

# Clean everything
echo "Cleaning GitHub pages repo..."
git rm -rf . || true

# Build docs
echo "Building docs..."
cd "$ROOTDIR"
make docs

# Copy built docs to public repo
echo "Copying built docs..."
cp -r artifacts/docs/html/* "$PUBLICDIR/"

# Commit
cd "$PUBLICDIR"
cat > .gitignore <<EOF
**/.doctrees
EOF
git add .gitignore
git add -A

CURRENT_COMMIT=$(git -C "$ROOTDIR" rev-parse HEAD)
if [ "$(git -C "$ROOTDIR" status --porcelain)" != "" ]; then
    CURRENT_COMMIT="${CURRENT_COMMIT}(dirty)"
fi
COMMIT_MSG="docs: $(date -u) - ${CURRENT_COMMIT}"

if [ "$(git status --porcelain)" != "" ]; then
    git commit -m "$COMMIT_MSG"
else
    echo "Nothing to commit"
fi

if $PUSH; then
    echo "Pushing to GitHub pages repo..."
    git push origin master
else
    echo "Not pushing. Use -P to push."
fi
