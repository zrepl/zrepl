#!/bin/bash
set -eo pipefail

GHPAGESREPO="git@github.com:zrepl/zrepl.github.io.git"
SCRIPTDIR=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )
PUBLICDIR="${SCRIPTDIR}/public_git"

checkout_repo_msg() {
    echo "clone ${GHPAGESREPO} to ${PUBLICDIR}:"
    echo "	git clone ${GHPAGESREPO} ${PUBLICDIR}"
    git clone "${GHPAGESREPO}" "${PUBLICDIR}"
}

exit_msg() {
    echo "error, exiting..."
}
trap exit_msg EXIT

cd "$SCRIPTDIR"

if [ ! -d "$PUBLICDIR" ]; then
    checkout_repo_msg
    exit 1
fi

echo -n "PRESS ENTER to confirm you commited the docs changes to the zrepl repo"
read

pushd "$PUBLICDIR" 

echo "verify we're in the GitHub pages repo..."
git remote get-url origin | grep -E "^${GHPAGESREPO}\$"
if [ "$?" -ne "0" ] ;then 
    checkout_repo_msg
    echo "finished checkout, please run again"
    exit 1
fi

echo "resetting GitHub pages repo to latest commit"
git fetch origin
git reset --hard origin/master

echo "cleaning GitHub pages repo"
git rm -rf .

popd

echo "building site"
set -e
make clean
make html
rsync -a _build/html/ public_git/
set +e

CURRENT_COMMIT=$(git rev-parse HEAD)
git status --porcelain
if [[ "$(git status --porcelain)" != "" ]]; then
    CURRENT_COMMIT="${CURRENT_COMMIT}(dirty)" 
fi
COMMIT_MSG="sphinx render from publish.sh - `date -u` - ${CURRENT_COMMIT}"

pushd "$PUBLICDIR"

echo "adding and commiting all changes in GitHub pages repo"
git add -A
git commit -m "$COMMIT_MSG"
git push origin master

