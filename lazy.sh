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

godep() {
    step "install build dependencies (versions pinned in build/go.mod and build/tools.go)"
    pushd "$(dirname "${BASH_SOURCE[0]}")"/build
    set -x
    export GO111MODULE=on # otherwise, a checkout of this repo in GOPATH will disable modules on Go 1.12 and earlier
    go build -v -mod=readonly -o "$GOPATH/bin/stringer"      golang.org/x/tools/cmd/stringer
    go build -v -mod=readonly -o "$GOPATH/bin/protoc-gen-go" github.com/golang/protobuf/protoc-gen-go
    go build -v -mod=readonly -o "$GOPATH/bin/enumer"        github.com/alvaroloes/enumer
    go build -v -mod=readonly -o "$GOPATH/bin/goimports"     golang.org/x/tools/cmd/goimports
    go build -v -mod=readonly -o "$GOPATH/bin/golangci-lint" github.com/golangci/golangci-lint/cmd/golangci-lint
    set +x
    popd
    if ! type stringer || ! type protoc-gen-go || ! type enumer || ! type goimports || ! type golangci-lint; then
        echo "Installed dependencies but can't find them in \$PATH, adjust it to contain \$GOPATH/bin" 1>&2
        exit 1
    fi
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
        godep|docdep|release_bins|docs)
            eval $cmd
            continue
            ;;
        devsetup)
            step "Installing development dependencies"
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

