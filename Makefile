.PHONY: generate build test vet cover release docs docs-clean clean release-bins
.DEFAULT_GOAL := build

ROOT := github.com/zrepl/zrepl
SUBPKGS := cmd logger rpc sshbytestream util

_TESTPKGS := $(ROOT) $(foreach p,$(SUBPKGS),$(ROOT)/$(p))

ARTIFACTDIR := artifacts

ifndef ZREPL_VERSION
    ZREPL_VERSION := $(shell git describe --dirty 2>/dev/null || echo "ZREPL_BUILD_INVALID_VERSION" )
    ifeq ($(ZREPL_VERSION),ZREPL_BUILD_INVALID_VERSION) # can't use .SHELLSTATUS because Debian Stretch is still on gmake 4.1
        $(error cannot infer variable ZREPL_VERSION using git and variable is not overriden by make invocation)
    endif
endif
GO_LDFLAGS := "-X github.com/zrepl/zrepl/cmd.zreplVersion=$(ZREPL_VERSION)"

GO_BUILD := go build -ldflags $(GO_LDFLAGS)

generate: #not part of the build, must do that manually
	@for pkg in $(_TESTPKGS); do\
		go generate "$$pkg" || exit 1; \
	done;

build:
		$(GO_BUILD) -o "$(ARTIFACTDIR)/zrepl"

test:
	@for pkg in $(_TESTPKGS); do \
		echo "Testing $$pkg"; \
		go test "$$pkg" || exit 1; \
	done;

vet:
	@for pkg in $(_TESTPKGS); do \
		echo "Vetting $$pkg"; \
		go vet "$$pkg" || exit 1; \
	done;

cover: artifacts
	@for pkg in $(_TESTPKGS); do \
		profile="$(ARTIFACTDIR)/cover-$$(basename $$pkg).out"; \
		go test -coverprofile "$$profile" $$pkg || exit 1; \
		if [ -f "$$profile" ]; then \
   			go tool cover -html="$$profile" -o "$${profile}.html" || exit 2; \
		fi; \
	done;

$(ARTIFACTDIR):
	mkdir -p "$@"

$(ARTIFACTDIR)/docs: $(ARTIFACTDIR)
	mkdir -p "$@"

docs: $(ARTIFACTDIR)/docs
	make -C docs \
		html \
		BUILDDIR=../artifacts/docs \

docs-clean:
	make -C docs \
		clean \
		BUILDDIR=../artifacts/docs

release-bins: $(ARTIFACTDIR) vet test
	GOOS=linux GOARCH=amd64   $(GO_BUILD) -o "$(ARTIFACTDIR)/zrepl-linux-amd64"
	GOOS=freebsd GOARCH=amd64 $(GO_BUILD) -o "$(ARTIFACTDIR)/zrepl-freebsd-amd64"

release: release-bins docs


clean: docs-clean
	rm -rf "$(ARTIFACTDIR)"
