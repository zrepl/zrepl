#!/bin/bash
set -eo pipefail

GHPAGESREPO="git@github.com:zrepl/zrepl.github.io.git"
SCRIPTDIR=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )
PUBLICDIR="${SCRIPTDIR}/public"

checkout_repo_msg() {
    echo "checkout ${GHPAGESREPO} to ${PUBLICDIR}:"
    echo "	git clone ${GHPAGESREPO} ${PUBLICDIR}"
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
    exit 1
fi

echo "resetting GitHub pages repo to latest commit"
git fetch origin
git reset --hard origin/master

echo "cleaning GitHub pages repo"
git clean -dn

popd

echo "building site"
hugo

pushd "$PUBLICDIR"

echo "adding and commiting all changes in GitHub pages repo"
git add -A
git commit -m "hugo render from publish.sh - `date -u`"
git push origin master

