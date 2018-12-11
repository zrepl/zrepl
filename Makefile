.PHONY: generate build test vet cover release docs docs-clean clean vendordeps
.DEFAULT_GOAL := build

ARTIFACTDIR := artifacts

ifdef ZREPL_VERSION
    _ZREPL_VERSION := $(ZREPL_VERSION)
endif
ifndef _ZREPL_VERSION
    _ZREPL_VERSION := $(shell git describe --always --dirty 2>/dev/null || echo "ZREPL_BUILD_INVALID_VERSION" )
    ifeq ($(_ZREPL_VERSION),ZREPL_BUILD_INVALID_VERSION) # can't use .SHELLSTATUS because Debian Stretch is still on gmake 4.1
        $(error cannot infer variable ZREPL_VERSION using git and variable is not overriden by make invocation)
    endif
endif
GO_LDFLAGS := "-X github.com/zrepl/zrepl/version.zreplVersion=$(_ZREPL_VERSION)"

GO_BUILD := go build -ldflags $(GO_LDFLAGS)

RELEASE_BINS := $(ARTIFACTDIR)/zrepl-freebsd-amd64
RELEASE_BINS += $(ARTIFACTDIR)/zrepl-linux-amd64
RELEASE_BINS += $(ARTIFACTDIR)/zrepl-darwin-amd64

RELEASE_NOARCH := $(ARTIFACTDIR)/zrepl-noarch.tar
THIS_PLATFORM_RELEASE_BIN := $(shell bash -c 'source <(go env) && echo "zrepl-$${GOOS}-$${GOARCH}"' )

vendordeps:
	dep ensure -v -vendor-only

generate: #not part of the build, must do that manually
	protoc -I=replication/pdu --go_out=plugins=grpc:replication/pdu replication/pdu/pdu.proto
	go generate -x ./...

build:
	@echo "INFO: In case of missing dependencies, run 'make vendordeps'"
	$(GO_BUILD) -o "$(ARTIFACTDIR)/zrepl"

test:
	go test ./...

vet:
	# for each supported platform to cover conditional compilation
	GOOS=linux   go vet ./...
	GOOS=darwin  go vet ./...
	GOOS=freebsd go vet ./...

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
		--transform 's/$(ARTIFACTDIR)/zrepl-$(_ZREPL_VERSION)-noarch/' \
		-acf $@ \
		$(ARTIFACTDIR)/docs/html \
		$(ARTIFACTDIR)/bash_completion \
		$(ARTIFACTDIR)/go_version.txt

release: $(RELEASE_BINS) $(RELEASE_NOARCH)
	rm -rf "$(ARTIFACTDIR)/release"
	mkdir -p "$(ARTIFACTDIR)/release"
	cp $^ "$(ARTIFACTDIR)/release"
	cd "$(ARTIFACTDIR)/release" && sha512sum $$(ls | sort) > sha512sum.txt
	@# note that we use ZREPL_VERSION and not _ZREPL_VERSION because we want to detect the override
	@if git describe --always --dirty 2>/dev/null | grep dirty >/dev/null; then \
        echo '[INFO] either git reports checkout is dirty or git is not installed or this is not a git checkout'; \
		if [ "$(ZREPL_VERSION)" = "" ]; then \
			echo '[WARN] git checkout is dirty and make variable ZREPL_VERSION was not used to override'; \
			exit 1; \
		fi; \
	fi;

clean: docs-clean
	rm -rf "$(ARTIFACTDIR)"
