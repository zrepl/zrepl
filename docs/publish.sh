#!/bin/bash
set -eo pipefail

GHPAGESREPO="git@github.com:zrepl/zrepl.github.io.git"

exit_msg() {
    echo "error, exiting..."
}
trap exit_msg EXIT

echo -n "PRESS ENTER to confirm you commited the docs changes to the zrepl repo"
read

cd public
echo "verify we're in the GitHub pages repo..."
git remote get-url origin | grep -E "^${GHPAGESREPO}\$"
echo "cleaning GitHub pages repo"
git clean -dn
cd ..
echo "building site"
hugo
cd public
echo "adding and commiting all changes in GitHub pages repo"
git add -A
git commit -m "hugo render from publish.sh - `date -u`"
git push origin master

