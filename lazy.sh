#!/bin/bash
set -e

if tty -s; then
    bold=$(tput bold)
    normal=$(tput sgr0)
else
    bold=""
    normal=""
fi

step() {
    echo "${bold}$1${normal}"
}

if ! type go; then
    step "go binary not installed or not in \$PATH" 1>&2
    exit 1
fi

if [ -z "$GOPATH" ]; then
    step "Make sure you have your GOPATH configured correctly" 1>&2
    exit 1
fi

CHECKOUTPATH="${GOPATH}/src/github.com/zrepl/zrepl"

clone() {
    step "Checkout sources to \$GOPATH/github.com/zrepl/zrepl"
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
}

builddep() {
    step "Install build depdencies using 'go get' to \$GOPATH/bin"
    go get -u golang.org/x/tools/cmd/stringer
    go get -u github.com/golang/dep/cmd/dep
    if ! type stringer || ! type dep; then
        echo "Installed dependencies but can't find them in \$PATH, adjust it to contain \$GOPATH/bin" 1>&2
        exit 1
    fi
}

godep() {
    step "Fetching dependencies using 'dep ensure'"
    dep ensure
}

docdep() {
    if ! type pip3; then
        step "pip3 binary not installed or not in \$PATH" 1>&2
        exit 1
    fi
    step "Installing doc build dependencies"
    local reqpath="${CHECKOUTPATH}/docs/requirements.txt"
    if [ ! -z "$ZREPL_LAZY_DOCS_REQPATH" ]; then
        reqpath="$ZREPL_LAZY_DOCS_REQPATH"
    fi
    pip3 install -r "$reqpath"
}

release() {
    step "Making release"
    make release
}

# precheck
for cmd in "$@"; do
    case "$cmd" in
        clone|builddep|godep|docdep|release_bins|docs)
            continue
            ;;
        devsetup)
            step "Installing development dependencies"
            builddep
            godep
            docdep
            step "Development dependencies installed"
            continue
            ;;
        *)
            step "Invalid command ${cmd}, exiting"
            exit 1
            ;;
    esac
done

for cmd in "$@"; do
    step "Step ${cmd}"
    eval $cmd
done

