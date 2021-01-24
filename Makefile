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
GOCOVMERGE := gocovmerge
RELEASE_DOCKER_BASEIMAGE_TAG ?= 1.15
RELEASE_DOCKER_BASEIMAGE ?= golang:$(RELEASE_DOCKER_BASEIMAGE_TAG)

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
	$(MAKE) test-go
	$(MAKE) bins-all
	$(MAKE) noarch
	$(MAKE) wrapup-and-checksum
	$(MAKE) check-git-clean
ifeq (SIGN, 1)
	$(make) sign
endif
	@echo "ZREPL RELEASE ARTIFACTS AVAILABLE IN artifacts/release"

release-docker: $(ARTIFACTDIR)
	sed 's/FROM.*!SUBSTITUTED_BY_MAKEFILE/FROM $(RELEASE_DOCKER_BASEIMAGE)/' build.Dockerfile > artifacts/release-docker.Dockerfile
	docker build -t zrepl_release --pull -f artifacts/release-docker.Dockerfile .
	docker run --rm -i -v $(CURDIR):/src -u $$(id -u):$$(id -g) \
		zrepl_release \
		make release GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM)

debs-docker:
	$(MAKE) _debs_or_rpms_docker _DEB_OR_RPM=deb
rpms-docker:
	$(MAKE) _debs_or_rpms_docker _DEB_OR_RPM=rpm
_debs_or_rpms_docker: # artifacts/_zrepl.zsh_completion artifacts/bash_completion docs zrepl-bin
	$(MAKE) $(_DEB_OR_RPM)-docker GOOS=linux GOARCH=amd64
	$(MAKE) $(_DEB_OR_RPM)-docker GOOS=linux GOARCH=arm64
	$(MAKE) $(_DEB_OR_RPM)-docker GOOS=linux GOARCH=arm GOARM=7
	$(MAKE) $(_DEB_OR_RPM)-docker GOOS=linux GOARCH=386

rpm: $(ARTIFACTDIR) # artifacts/_zrepl.zsh_completion artifacts/bash_completion docs zrepl-bin
	$(eval _ZREPL_RPM_VERSION := $(subst -,.,$(_ZREPL_VERSION)))
	$(eval _ZREPL_RPM_TOPDIR_ABS := $(CURDIR)/$(ARTIFACTDIR)/rpmbuild)
	rm -rf "$(_ZREPL_RPM_TOPDIR_ABS)"
	mkdir "$(_ZREPL_RPM_TOPDIR_ABS)"
	mkdir -p "$(_ZREPL_RPM_TOPDIR_ABS)"/{SPECS,RPMS,BUILD,BUILDROOT}
	sed "s/^Version:.*/Version:          $(_ZREPL_RPM_VERSION)/g" \
	    packaging/rpm/zrepl.spec >  $(_ZREPL_RPM_TOPDIR_ABS)/SPECS/zrepl.spec

	# see /usr/lib/rpm/platform
ifeq ($(GOARCH),amd64)
	$(eval _ZREPL_RPMBUILD_TARGET := x86_64)
else ifeq ($(GOARCH), 386)
	$(eval _ZREPL_RPMBUILD_TARGET := i386)
else ifeq ($(GOARCH), arm64)
	$(eval _ZREPL_RPMBUILD_TARGET := aarch64)
else ifeq ($(GOARCH), arm)
	$(eval _ZREPL_RPMBUILD_TARGET := armv7hl)
else
	$(eval _ZREPL_RPMBUILD_TARGET := $(GOARCH))
endif
	rpmbuild \
		--build-in-place \
		--define "_sourcedir $(CURDIR)" \
		--define "_topdir $(_ZREPL_RPM_TOPDIR_ABS)" \
		--define "_zrepl_binary_filename zrepl-$(ZREPL_TARGET_TUPLE)" \
		--target $(_ZREPL_RPMBUILD_TARGET) \
		-bb "$(_ZREPL_RPM_TOPDIR_ABS)"/SPECS/zrepl.spec
	cp "$(_ZREPL_RPM_TOPDIR_ABS)"/RPMS/$(_ZREPL_RPMBUILD_TARGET)/zrepl-$(_ZREPL_RPM_VERSION)*.rpm $(ARTIFACTDIR)/

rpm-docker:
	docker build -t zrepl_rpm_pkg --pull -f packaging/rpm/Dockerfile .
	docker run --rm -i -v $(CURDIR):/build/src -u $$(id -u):$$(id -g) \
		zrepl_rpm_pkg \
		make rpm GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM)

deb: $(ARTIFACTDIR) # artifacts/_zrepl.zsh_completion artifacts/bash_completion docs zrepl-bin

	cp packaging/deb/debian/changelog.template packaging/deb/debian/changelog
	sed -i 's/DATE_DASH_R_OUTPUT/$(shell date -R)/' packaging/deb/debian/changelog
	VERSION="$(subst -,.,$(_ZREPL_VERSION))"; \
		export VERSION="$${VERSION#v}"; \
		sed -i 's/VERSION/'"$$VERSION"'/' packaging/deb/debian/changelog

ifeq ($(GOARCH), arm)
	$(eval  DEB_HOST_ARCH := armhf)
else ifeq ($(GOARCH), 386)
	$(eval DEB_HOST_ARCH := i386)
else
	$(eval DEB_HOST_ARCH := $(GOARCH))
endif

	export ZREPL_DPKG_ZREPL_BINARY_FILENAME=zrepl-$(ZREPL_TARGET_TUPLE); \
		dpkg-buildpackage -b --no-sign --host-arch $(DEB_HOST_ARCH)
	cp ../*.deb artifacts/

deb-docker:
	docker build -t zrepl_debian_pkg --pull -f packaging/deb/Dockerfile .
	docker run --rm -i -v $(CURDIR):/build/src -u $$(id -u):$$(id -g) \
		zrepl_debian_pkg \
		make deb GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM)

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
		$(ARTIFACTDIR)/_zrepl.zsh_completion \
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
.PHONY: bins-all lint test-go test-platform cover-merge cover-html vet zrepl-bin test-platform-bin generate-platform-test-list

BINS_ALL_TARGETS := zrepl-bin test-platform-bin vet lint
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

vet:
	$(GO_ENV_VARS) $(GO) vet $(GO_BUILDFLAGS) ./...

test-go: $(ARTIFACTDIR)
	rm -f "$(ARTIFACTDIR)/gotest.cover"
ifeq ($(COVER),1)
	$(GO_ENV_VARS) $(GO) test $(GO_BUILDFLAGS) \
		-coverpkg github.com/zrepl/zrepl/... \
		-covermode atomic \
		-coverprofile "$(ARTIFACTDIR)/gotest.cover" \
		./...
else
	$(GO_ENV_VARS) $(GO) test $(GO_BUILDFLAGS) \
		./...
endif

zrepl-bin:
	$(GO_BUILD) -o "$(ARTIFACTDIR)/zrepl-$(ZREPL_TARGET_TUPLE)"

generate-platform-test-list:
	$(GO_BUILD) -o $(ARTIFACTDIR)/generate-platform-test-list ./platformtest/tests/gen

COVER_PLATFORM_BIN_PATH := $(ARTIFACTDIR)/platformtest-cover-$(ZREPL_TARGET_TUPLE)
cover-platform-bin:
	$(GO_ENV_VARS) $(GO) test $(GO_BUILDFLAGS) \
		-c -o "$(COVER_PLATFORM_BIN_PATH)" \
		-covermode=atomic -cover -coverpkg github.com/zrepl/zrepl/... \
		./platformtest/harness
cover-platform:
	# do not track dependency on cover-platform-bin to allow build of binary outside of test VM
	export _TEST_PLATFORM_CMD="$(COVER_PLATFORM_BIN_PATH) \
		-test.coverprofile \"$(ARTIFACTDIR)/platformtest.cover\" \
		-test.v \
		__DEVEL--i-heard-you-like-tests"; \
		$(MAKE) _test-or-cover-platform-impl

TEST_PLATFORM_BIN_PATH := $(ARTIFACTDIR)/platformtest-$(ZREPL_TARGET_TUPLE)
test-platform-bin:
	$(GO_BUILD) -o "$(TEST_PLATFORM_BIN_PATH)" ./platformtest/harness
test-platform:
	export _TEST_PLATFORM_CMD="\"$(TEST_PLATFORM_BIN_PATH)\""; \
		$(MAKE) _test-or-cover-platform-impl

ZREPL_PLATFORMTEST_POOLNAME := zreplplatformtest
ZREPL_PLATFORMTEST_IMAGEPATH := /tmp/zreplplatformtest.pool.img
ZREPL_PLATFORMTEST_MOUNTPOINT := /tmp/zreplplatformtest.pool
ZREPL_PLATFORMTEST_ZFS_LOG := /tmp/zreplplatformtest.zfs.log
# ZREPL_PLATFORMTEST_STOP_AND_KEEP := -failure.stop-and-keep-pool
ZREPL_PLATFORMTEST_ARGS :=
_test-or-cover-platform-impl: $(ARTIFACTDIR)
ifndef _TEST_PLATFORM_CMD
	$(error _TEST_PLATFORM_CMD is undefined, caller 'cover-platform' or 'test-platform' should have defined it)
endif
	rm -f "$(ZREPL_PLATFORMTEST_ZFS_LOG)"
	rm -f "$(ARTIFACTDIR)/platformtest.cover"
	platformtest/logmockzfs/logzfsenv "$(ZREPL_PLATFORMTEST_ZFS_LOG)" `which zfs` \
		$(_TEST_PLATFORM_CMD) \
		-poolname "$(ZREPL_PLATFORMTEST_POOLNAME)" \
		-imagepath "$(ZREPL_PLATFORMTEST_IMAGEPATH)" \
		-mountpoint "$(ZREPL_PLATFORMTEST_MOUNTPOINT)" \
		$(ZREPL_PLATFORMTEST_STOP_AND_KEEP) \
		$(ZREPL_PLATFORMTEST_ARGS)

cover-merge: $(ARTIFACTDIR)
	$(GOCOVMERGE) $(ARTIFACTDIR)/platformtest.cover $(ARTIFACTDIR)/gotest.cover > $(ARTIFACTDIR)/merged.cover
cover-html: cover-merge
	$(GO) tool cover -html "$(ARTIFACTDIR)/merged.cover" -o "$(ARTIFACTDIR)/merged.cover.html"

cover-full:
	test "$$(id -u)" = "0" || echo "MUST RUN AS ROOT" 1>&2
	$(MAKE) test-go COVER=1
	$(MAKE) cover-platform-bin
	$(MAKE) cover-platform
	$(MAKE) cover-html

##################### DEV TARGETS #####################
# not part of the build, must do that manually
.PHONY: generate formatcheck format

generate: generate-platform-test-list
	protoc -I=replication/logic/pdu --go_out=replication/logic/pdu --go-grpc_out=replication/logic/pdu replication/logic/pdu/pdu.proto
	protoc -I=rpc/grpcclientidentity/example --go_out=rpc/grpcclientidentity/example/pdu --go-grpc_out=rpc/grpcclientidentity/example/pdu rpc/grpcclientidentity/example/grpcauth.proto
	$(GO_ENV_VARS) $(GO) generate $(GO_BUILDFLAGS) -x ./...

GOIMPORTS := goimports -srcdir . -local 'github.com/zrepl/zrepl'
FINDSRCFILES := find . -type f -name '*.go' -not -path "./vendor/*" -not -name '*.pb.go' -not -name '*_enumer.go'

formatcheck:
	@# goimports doesn't have a knob to exit with non-zero status code if formatting is needed
	@# see https://go-review.googlesource.com/c/tools/+/237378
	@ affectedfiles=$$($(GOIMPORTS) -l $(shell $(FINDSRCFILES)) | tee /dev/stderr | wc -l); test "$$affectedfiles" = 0

format:
	@ $(GOIMPORTS) -w -d $(shell  $(FINDSRCFILES))

##################### NOARCH #####################
.PHONY: noarch $(ARTIFACTDIR)/bash_completion $(ARTIFACTDIR)/_zrepl.zsh_completion $(ARTIFACTDIR)/go_env.txt docs docs-clean


$(ARTIFACTDIR):
	mkdir -p "$@"
$(ARTIFACTDIR)/docs: $(ARTIFACTDIR)
	mkdir -p "$@"

noarch: $(ARTIFACTDIR)/bash_completion $(ARTIFACTDIR)/_zrepl.zsh_completion $(ARTIFACTDIR)/go_env.txt docs
	# pass

$(ARTIFACTDIR)/bash_completion:
	$(MAKE) zrepl-bin GOOS=$(GOHOSTOS) GOARCH=$(GOHOSTARCH)
	artifacts/zrepl-$(GOHOSTOS)-$(GOHOSTARCH) gencompletion bash "$@"

$(ARTIFACTDIR)/_zrepl.zsh_completion:
	$(MAKE) zrepl-bin GOOS=$(GOHOSTOS) GOARCH=$(GOHOSTARCH)
	artifacts/zrepl-$(GOHOSTOS)-$(GOHOSTARCH) gencompletion zsh "$@"

$(ARTIFACTDIR)/go_env.txt:
	$(GO_ENV_VARS) $(GO) env > $@
	$(GO) version >> $@

docs: $(ARTIFACTDIR)/docs
	# https://www.sphinx-doc.org/en/master/man/sphinx-build.html
	make -C docs \
		html \
		BUILDDIR=../artifacts/docs \
		SPHINXOPTS="-W --keep-going -n"

docs-clean:
	make -C docs \
		clean \
		BUILDDIR=../artifacts/docs
