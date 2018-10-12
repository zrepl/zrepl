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

if ! type go >/dev/null; then
    step "go binary not installed or not in \$PATH" 1>&2
    exit 1
fi

if [ -z "$GOPATH" ]; then
    step "Make sure you have your GOPATH configured correctly" 1>&2
    exit 1
fi

CHECKOUTPATH="${GOPATH}/src/github.com/zrepl/zrepl"

builddep() {
    step "Install build depdencies using 'go get' to \$GOPATH/bin"
    go get -u golang.org/x/tools/cmd/stringer
    go get -u github.com/golang/dep/cmd/dep
    go get -u github.com/golang/protobuf/protoc-gen-go
    go get -u github.com/alvaroloes/enumer
    if ! type stringer || ! type dep || ! type protoc-gen-go; || ! type enumer then
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

for cmd in "$@"; do
    case "$cmd" in
        builddep|godep|docdep|release_bins|docs)
            eval $cmd
            continue
            ;;
        devsetup)
            step "Installing development dependencies"
            builddep
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

