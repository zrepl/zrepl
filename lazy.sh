#!/bin/sh
set -e

if [ ! -z "$TERM" ]; then
    bold=$(tput bold)
    normal=$(tput sgr0)
else
    bold=""
    normal=""
fi

step() {
    echo "${bold}$1${normal}"
}

if [ -z "$GOPATH" ]; then
    step "Make sure you have your GOPATH configured correctly" 1>&2
    exit 1
fi

step "Checkout sources to \$GOPATH/github.com/zrepl/zrepl"
CHECKOUTPATH="${GOPATH}/src/github.com/zrepl/zrepl"
if [ -e "$CHECKOUTPATH" ]; then
    echo "${CHECKOUTPATH} already exists"
    if [ ! -d "$CHECKOUTPATH" ]; then
        echo "${CHECKOUTPATH} is not a directory, aborting" 1>&2
    else
        cd "$CHECKOUTPATH"
    fi
else
    mkdir -p "$GOPATH/src/github.com/zrepl"
    cd "$GOPATH/src/github.com/zrepl"
    git clone https://github.com/zrepl/zrepl.git
    cd zrepl
fi

step "Install build depdencies using 'go get' to \$GOPATH/bin"
go get -u golang.org/x/tools/cmd/stringer
go get -u github.com/golang/dep/cmd/dep
if ! type stringer || ! type dep; then
    echo "Installed dependencies but can't find them in \$PATH, adjust it to contain \$GOPATH/bin" 1>&2
    exit 1
fi

step "Fetching dependencies using 'dep ensure'"
dep ensure

step "Making release"
make release-bins

step "Release artifacts are available in $(pwd)/artifacts"
