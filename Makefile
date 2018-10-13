.PHONY: generate build test vet cover release docs docs-clean clean vendordeps
.DEFAULT_GOAL := build

ROOT := github.com/zrepl/zrepl
SUBPKGS += client
SUBPKGS += config
SUBPKGS += daemon
SUBPKGS += daemon/filters
SUBPKGS += daemon/job
SUBPKGS += daemon/logging
SUBPKGS += daemon/nethelpers
SUBPKGS += daemon/pruner
SUBPKGS += daemon/snapper
SUBPKGS += daemon/streamrpcconfig
SUBPKGS += daemon/transport
SUBPKGS += daemon/transport/connecter
SUBPKGS += daemon/transport/serve
SUBPKGS += endpoint
SUBPKGS += logger
SUBPKGS += pruning
SUBPKGS += pruning/retentiongrid
SUBPKGS += replication
SUBPKGS += replication/fsrep
SUBPKGS += replication/pdu
SUBPKGS += replication/internal/queue
SUBPKGS += replication/internal/diff
SUBPKGS += tlsconf
SUBPKGS += util
SUBPKGS += version
SUBPKGS += zfs

_TESTPKGS := $(ROOT) $(foreach p,$(SUBPKGS),$(ROOT)/$(p))

ARTIFACTDIR := artifacts

ifndef ZREPL_VERSION
    ZREPL_VERSION := $(shell git describe --dirty 2>/dev/null || echo "ZREPL_BUILD_INVALID_VERSION" )
    ifeq ($(ZREPL_VERSION),ZREPL_BUILD_INVALID_VERSION) # can't use .SHELLSTATUS because Debian Stretch is still on gmake 4.1
        $(error cannot infer variable ZREPL_VERSION using git and variable is not overriden by make invocation)
    endif
endif
GO_LDFLAGS := "-X github.com/zrepl/zrepl/version.zreplVersion=$(ZREPL_VERSION)"

GO_BUILD := go build -ldflags $(GO_LDFLAGS)

RELEASE_BINS := $(ARTIFACTDIR)/zrepl-freebsd-amd64
RELEASE_BINS += $(ARTIFACTDIR)/zrepl-linux-amd64
RELEASE_BINS += $(ARTIFACTDIR)/zrepl-darwin-amd64

RELEASE_NOARCH := $(ARTIFACTDIR)/zrepl-noarch.tar
THIS_PLATFORM_RELEASE_BIN := $(shell bash -c 'source <(go env) && echo "zrepl-$${GOOS}-$${GOARCH}"' )

vendordeps:
	dep ensure -v -vendor-only

generate: #not part of the build, must do that manually
	protoc -I=replication/pdu --go_out=replication/pdu replication/pdu/pdu.proto
	@for pkg in $(_TESTPKGS); do\
		go generate "$$pkg" || exit 1; \
	done;

build:
	@echo "INFO: In case of missing dependencies, run 'make vendordeps'"
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

$(ARTIFACTDIR)/bash_completion: $(RELEASE_BINS)
	artifacts/$(THIS_PLATFORM_RELEASE_BIN) bashcomp "$@"

$(ARTIFACTDIR)/go_version.txt:
	go version > $@

docs: $(ARTIFACTDIR)/docs
	make -C docs \
		html \
		BUILDDIR=../artifacts/docs \

docs-clean:
	make -C docs \
		clean \
		BUILDDIR=../artifacts/docs


.PHONY: $(RELEASE_BINS)
# TODO: two wildcards possible
$(RELEASE_BINS): $(ARTIFACTDIR)/zrepl-%-amd64: generate $(ARTIFACTDIR) vet test
	@echo "INFO: In case of missing dependencies, run 'make vendordeps'"
	GOOS=$* GOARCH=amd64   $(GO_BUILD) -o "$(ARTIFACTDIR)/zrepl-$*-amd64"

$(RELEASE_NOARCH): docs $(ARTIFACTDIR)/bash_completion $(ARTIFACTDIR)/go_version.txt
	tar --mtime='1970-01-01' --sort=name \
		--transform 's/$(ARTIFACTDIR)/zrepl-$(ZREPL_VERSION)-noarch/' \
		-acf $@ \
		$(ARTIFACTDIR)/docs/html \
		$(ARTIFACTDIR)/bash_completion \
		$(ARTIFACTDIR)/go_version.txt

release: $(RELEASE_BINS) $(RELEASE_NOARCH)
	rm -rf "$(ARTIFACTDIR)/release"
	mkdir -p "$(ARTIFACTDIR)/release"
	cp $^ "$(ARTIFACTDIR)/release"
	cd "$(ARTIFACTDIR)/release" && sha512sum $$(ls | sort) > sha512sum.txt
	@if echo "$(ZREPL_VERSION)" | grep dirty > /dev/null; then\
		echo '[WARN] Do not publish the artifacts, make variable ZREPL_VERSION=$(ZREPL_VERSION) indicates they are dirty!'; \
		exit 1; \
	fi

clean: docs-clean
	rm -rf "$(ARTIFACTDIR)"
