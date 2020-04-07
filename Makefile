.PHONY: generate build test vet cover release docs docs-clean clean format lint platformtest
.PHONY: release bins-all release-noarch
.DEFAULT_GOAL := zrepl-bin

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

GO := go
GOOS ?= $(shell bash -c 'source <($(GO) env) && echo "$$GOOS"')
GOARCH ?= $(shell bash -c 'source <($(GO) env) && echo "$$GOARCH"')
GOARM ?= $(shell bash -c 'source <($(GO) env) && echo "$$GOARM"')
GOHOSTOS ?= $(shell bash -c 'source <($(GO) env) && echo "$$GOHOSTOS"')
GOHOSTARCH ?= $(shell bash -c 'source <($(GO) env) && echo "$$GOHOSTARCH"')
GO_ENV_VARS := GO111MODULE=on
GO_LDFLAGS := "-X github.com/zrepl/zrepl/version.zreplVersion=$(_ZREPL_VERSION)"
GO_MOD_READONLY := -mod=readonly
GO_EXTRA_BUILDFLAGS :=
GO_BUILDFLAGS := $(GO_MOD_READONLY) $(GO_EXTRA_BUILDFLAGS)
GO_BUILD := $(GO_ENV_VARS) $(GO) build $(GO_BUILDFLAGS) -ldflags $(GO_LDFLAGS)
GOLANGCI_LINT := golangci-lint
ifneq ($(GOARM),)
	ZREPL_TARGET_TUPLE := $(GOOS)-$(GOARCH)v$(GOARM)
else
	ZREPL_TARGET_TUPLE := $(GOOS)-$(GOARCH)
endif

.PHONY: printvars
printvars:
	@echo GOOS=$(GOOS)
	@echo GOARCH=$(GOARCH)
	@echo GOARM=$(GOARM)


##################### PRODUCING A RELEASE #############
.PHONY: release wrapup-and-checksum check-git-clean sign clean

release: clean
	# no cross-platform support for target test
	$(MAKE) test
	$(MAKE) bins-all
	$(MAKE) noarch
	$(MAKE) wrapup-and-checksum
	$(MAKE) check-git-clean
ifeq (SIGN, 1)
	$(make) sign
endif
	@echo "ZREPL RELEASE ARTIFACTS AVAILABLE IN artifacts/release"

# expects `release` target to have run before
NOARCH_TARBALL := $(ARTIFACTDIR)/zrepl-noarch.tar
wrapup-and-checksum:
	rm -f $(NOARCH_TARBALL)
	tar --mtime='1970-01-01' --sort=name \
		--transform 's/$(ARTIFACTDIR)/zrepl-$(_ZREPL_VERSION)-noarch/' \
		--transform 's#dist#zrepl-$(_ZREPL_VERSION)-noarch/dist#' \
		--transform 's#config/samples#zrepl-$(_ZREPL_VERSION)-noarch/config#' \
		-acf $(NOARCH_TARBALL) \
		$(ARTIFACTDIR)/docs/html \
		$(ARTIFACTDIR)/bash_completion \
		$(ARTIFACTDIR)/go_env.txt \
		dist \
		config/samples
	rm -rf "$(ARTIFACTDIR)/release"
	mkdir -p "$(ARTIFACTDIR)/release"
	cp -l $(ARTIFACTDIR)/zrepl-* \
		$(ARTIFACTDIR)/platformtest-* \
		"$(ARTIFACTDIR)/release"
	cd "$(ARTIFACTDIR)/release" && sha512sum $$(ls | sort) > sha512sum.txt

check-git-clean:
	@# note that we use ZREPL_VERSION and not _ZREPL_VERSION because we want to detect the override
	@if git describe --always --dirty 2>/dev/null | grep dirty >/dev/null; then \
        echo '[INFO] either git reports checkout is dirty or git is not installed or this is not a git checkout'; \
		if [ "$(ZREPL_VERSION)" = "" ]; then \
			echo '[WARN] git checkout is dirty and make variable ZREPL_VERSION was not used to override'; \
			git status; \
			echo "git diff:";  \
			git diff | cat; \
			exit 1; \
		fi; \
	fi;

sign:
	gpg -u "89BC 5D89 C845 568B F578  B306 CDBD 8EC8 E27C A5FC" \
		--armor \
		--detach-sign $(ARTIFACTDIR)/release/sha512sum.txt

clean: docs-clean
	rm -rf "$(ARTIFACTDIR)"

##################### BINARIES #####################
.PHONY: bins-all lint test vet zrepl-bin platformtest-bin

BINS_ALL_TARGETS := zrepl-bin platformtest-bin vet lint
GO_SUPPORTS_ILLUMOS := $(shell $(GO) version | gawk -F '.' '/^go version /{split($$0, comps, " "); split(comps[3], v, "."); if (v[1] == "go1" && v[2] >= 13) { print "illumos"; } else { print "noillumos"; }}')
bins-all:
	$(MAKE) $(BINS_ALL_TARGETS) GOOS=freebsd   GOARCH=amd64
	$(MAKE) $(BINS_ALL_TARGETS) GOOS=freebsd   GOARCH=386
	$(MAKE) $(BINS_ALL_TARGETS) GOOS=linux     GOARCH=amd64
	$(MAKE) $(BINS_ALL_TARGETS) GOOS=linux     GOARCH=arm64
	$(MAKE) $(BINS_ALL_TARGETS) GOOS=linux     GOARCH=arm     GOARM=7
	$(MAKE) $(BINS_ALL_TARGETS) GOOS=linux     GOARCH=386
	$(MAKE) $(BINS_ALL_TARGETS) GOOS=darwin    GOARCH=amd64
	$(MAKE) $(BINS_ALL_TARGETS) GOOS=solaris   GOARCH=amd64
ifeq ($(GO_SUPPORTS_ILLUMOS), illumos)
	$(MAKE) $(BINS_ALL_TARGETS) GOOS=illumos   GOARCH=amd64
else ifeq ($(GO_SUPPORTS_ILLUMOS), noillumos)
	@echo "SKIPPING ILLUMOS BUILD BECAUSE GO VERSION DOESN'T SUPPORT IT"
else
	@echo "CANNOT DETERMINE WHETHER GO VERSION SUPPORTS GOOS=illumos"; exit 1
endif

lint:
	$(GO_ENV_VARS) $(GOLANGCI_LINT) run ./...
test:
	$(GO_ENV_VARS) $(GO) test $(GO_BUILDFLAGS) ./...
vet:
	$(GO_ENV_VARS) $(GO) vet $(GO_BUILDFLAGS) ./...

zrepl-bin:
	$(GO_BUILD) -o "$(ARTIFACTDIR)/zrepl-$(ZREPL_TARGET_TUPLE)"

platformtest-bin:
	$(GO_BUILD) -o "$(ARTIFACTDIR)/platformtest-$(ZREPL_TARGET_TUPLE)" ./platformtest/harness

##################### DEV TARGETS #####################
# not part of the build, must do that manually
.PHONY: generate format platformtest

generate:
	protoc -I=replication/logic/pdu --go_out=plugins=grpc:replication/logic/pdu replication/logic/pdu/pdu.proto
	$(GO_ENV_VARS) $(GO) generate $(GO_BUILDFLAGS) -x ./...

format:
	goimports -srcdir . -local 'github.com/zrepl/zrepl' -w $(shell find . -type f -name '*.go' -not -path "./vendor/*" -not -name '*.pb.go' -not -name '*_enumer.go')

ZREPL_PLATFORMTEST_POOLNAME := zreplplatformtest
ZREPL_PLATFORMTEST_IMAGEPATH := /tmp/zreplplatformtest.pool.img
ZREPL_PLATFORMTEST_MOUNTPOINT := /tmp/zreplplatformtest.pool
ZREPL_PLATFORMTEST_ZFS_LOG := /tmp/zreplplatformtest.zfs.log
# ZREPL_PLATFORMTEST_STOP_AND_KEEP := -failure.stop-and-keep-pool
ZREPL_PLATFORMTEST_ARGS := 
platformtest: # do not track dependency on platformtest-bin to allow build of platformtest outside of test VM
	rm -f "$(ZREPL_PLATFORMTEST_ZFS_LOG)"
	platformtest/logmockzfs/logzfsenv "$(ZREPL_PLATFORMTEST_ZFS_LOG)" `which zfs` \
	"$(ARTIFACTDIR)/platformtest-$(ZREPL_TARGET_TUPLE)" \
		-poolname "$(ZREPL_PLATFORMTEST_POOLNAME)" \
		-imagepath "$(ZREPL_PLATFORMTEST_IMAGEPATH)" \
		-mountpoint "$(ZREPL_PLATFORMTEST_MOUNTPOINT)" \
		$(ZREPL_PLATFORMTEST_STOP_AND_KEEP) \
		$(ZREPL_PLATFORMTEST_ARGS)

##################### NOARCH #####################
.PHONY: noarch $(ARTIFACTDIR)/bash_completion $(ARTIFACTDIR)/go_env.txt docs docs-clean


$(ARTIFACTDIR):
	mkdir -p "$@"
$(ARTIFACTDIR)/docs: $(ARTIFACTDIR)
	mkdir -p "$@"

noarch: $(ARTIFACTDIR)/bash_completion $(ARTIFACTDIR)/go_env.txt docs
	# pass

$(ARTIFACTDIR)/bash_completion:
	$(MAKE) zrepl-bin GOOS=$(GOHOSTOS) GOARCH=$(GOHOSTARCH)
	artifacts/zrepl-$(GOHOSTOS)-$(GOHOSTARCH) bashcomp "$@"

$(ARTIFACTDIR)/go_env.txt:
	$(GO_ENV_VARS) $(GO) env > $@

docs: $(ARTIFACTDIR)/docs
	make -C docs \
		html \
		BUILDDIR=../artifacts/docs \

docs-clean:
	make -C docs \
		clean \
		BUILDDIR=../artifacts/docs
