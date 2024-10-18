.PHONY: generate build test vet cover release docs docs-clean clean format lint platformtest
.PHONY: release release-noarch
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

ZREPL_PACKAGE_RELEASE := 1

GO := go
GOOS ?= $(shell bash -c 'source <($(GO) env) && echo "$$GOOS"')
GOARCH ?= $(shell bash -c 'source <($(GO) env) && echo "$$GOARCH"')
GOARM ?= $(shell bash -c 'source <($(GO) env) && echo "$$GOARM"')
GOHOSTOS ?= $(shell bash -c 'source <($(GO) env) && echo "$$GOHOSTOS"')
GOHOSTARCH ?= $(shell bash -c 'source <($(GO) env) && echo "$$GOHOSTARCH"')
GO_ENV_VARS := CGO_ENABLED=0
GO_LDFLAGS := "-X github.com/zrepl/zrepl/version.zreplVersion=$(_ZREPL_VERSION)"
GO_MOD_READONLY := -mod=readonly
GO_EXTRA_BUILDFLAGS :=
GO_BUILDFLAGS := $(GO_MOD_READONLY) $(GO_EXTRA_BUILDFLAGS)
GO_BUILD := $(GO_ENV_VARS) $(GO) build $(GO_BUILDFLAGS) -ldflags $(GO_LDFLAGS)
GOLANGCI_LINT := golangci-lint
GOCOVMERGE := gocovmerge
RELEASE_GOVERSION ?= go1.23.1
STRIPPED_GOVERSION := $(subst go,,$(RELEASE_GOVERSION))
RELEASE_DOCKER_BASEIMAGE ?= golang:$(STRIPPED_GOVERSION)
RELEASE_DOCKER_CACHEMOUNT :=

ifneq ($(GOARM),)
	ZREPL_TARGET_TUPLE := $(GOOS)-$(GOARCH)v$(GOARM)
else
	ZREPL_TARGET_TUPLE := $(GOOS)-$(GOARCH)
endif


ifneq ($(RELEASE_DOCKER_CACHEMOUNT),)
	_RELEASE_DOCKER_CACHEMOUNT := -v $(RELEASE_DOCKER_CACHEMOUNT)/mod:/go/pkg/mod -v $(RELEASE_DOCKER_CACHEMOUNT)/xdg-cache:/root/.cache/go-build
.PHONY: release-docker-mkcachemount
release-docker-mkcachemount:
	mkdir -p $(RELEASE_DOCKER_CACHEMOUNT)
	mkdir -p $(RELEASE_DOCKER_CACHEMOUNT)/mod
	mkdir -p $(RELEASE_DOCKER_CACHEMOUNT)/xdg-cache
else
	_RELEASE_DOCKER_CACHEMOUNT :=
.PHONY: release-docker-mkcachemount
release-docker-mkcachemount:
	# nothing to do
endif

##################### PRODUCING A RELEASE #############
.PHONY: release wrapup-and-checksum check-git-clean sign clean ensure-release-toolchain

ensure-release-toolchain:
	# ensure the toolchain is actually the one we expect
	test $(RELEASE_GOVERSION) = "$$($(GO_ENV_VARS) $(GO) env GOVERSION)"

release: ensure-release-toolchain
	$(MAKE) _run_make_foreach_target_tuple RUN_MAKE_FOREACH_TARGET_TUPLE_ARG="vet"
	$(MAKE) _run_make_foreach_target_tuple RUN_MAKE_FOREACH_TARGET_TUPLE_ARG="lint"
	$(MAKE) _run_make_foreach_target_tuple RUN_MAKE_FOREACH_TARGET_TUPLE_ARG="zrepl-bin"
	$(MAKE) _run_make_foreach_target_tuple RUN_MAKE_FOREACH_TARGET_TUPLE_ARG="test-platform-bin"
	$(MAKE) noarch

release-docker: $(ARTIFACTDIR) release-docker-mkcachemount
	sed 's/FROM.*!SUBSTITUTED_BY_MAKEFILE/FROM $(RELEASE_DOCKER_BASEIMAGE)/' build.Dockerfile > $(ARTIFACTDIR)/build.Dockerfile
	docker build -t zrepl_release --pull -f $(ARTIFACTDIR)/build.Dockerfile .
	docker run --rm -i \
		$(_RELEASE_DOCKER_CACHEMOUNT) \
		-v $(CURDIR):/src -u $$(id -u):$$(id -g) \
		zrepl_release \
		make release \
			GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM) \
			ZREPL_VERSION=$(ZREPL_VERSION) ZREPL_PACKAGE_RELEASE=$(ZREPL_PACKAGE_RELEASE) \
			RELEASE_GOVERSION=$(RELEASE_GOVERSION)

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
	for d in BUILD BUILDROOT RPMS SOURCES SPECS SRPMS; do \
		mkdir -p "$(_ZREPL_RPM_TOPDIR_ABS)/$$d"; \
	done
	sed \
		-e "s/^Version:.*/Version:          $(_ZREPL_RPM_VERSION)/g" \
		-e "s/^Release:.*/Release:          $(ZREPL_PACKAGE_RELEASE)/g" \
	    packaging/rpm/zrepl.spec \
		>  $(_ZREPL_RPM_TOPDIR_ABS)/SPECS/zrepl.spec

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
		make rpm \
			GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM) \
			ZREPL_VERSION=$(ZREPL_VERSION) ZREPL_PACKAGE_RELEASE=$(ZREPL_PACKAGE_RELEASE)


deb: $(ARTIFACTDIR) # artifacts/_zrepl.zsh_completion artifacts/bash_completion docs zrepl-bin

	cp packaging/deb/debian/changelog.template packaging/deb/debian/changelog
	sed -i 's/DATE_DASH_R_OUTPUT/$(shell date -R)/' packaging/deb/debian/changelog
	VERSION="$(subst -,.,$(_ZREPL_VERSION))-$(ZREPL_PACKAGE_RELEASE)"; \
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
	# Use a small open file limit to make fakeroot work. If we don't
	# specify it, docker daemon will use its file limit. I don't know
	# what changed (Docker, its systemd service, its Go version). But I
	# observed fakeroot iterating close(i) up to i > 1000000, which costs
	# a good amount of CPU time and makes the build slow.
	docker run --rm -i -v $(CURDIR):/build/src -u $$(id -u):$$(id -g) \
		--ulimit nofile=1024:1024 \
		zrepl_debian_pkg \
		make deb \
			GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM) \
			ZREPL_VERSION=$(ZREPL_VERSION) ZREPL_PACKAGE_RELEASE=$(ZREPL_PACKAGE_RELEASE)

# expects `release`, `deb` & `rpm` targets to have run before
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
	cp -l $(ARTIFACTDIR)/zrepl* \
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

tag-release:
	test -n "$(ZREPL_TAG_VERSION)" || exit 1
	git tag -u '328A6627FA98061D!' -m "$(ZREPL_TAG_VERSION)" "$(ZREPL_TAG_VERSION)"

sign:
	gpg -u '328A6627FA98061D!' \
		--armor \
		--detach-sign $(ARTIFACTDIR)/release/sha512sum.txt

clean: docs-clean
	rm -rf "$(ARTIFACTDIR)"

download-circleci-release:
	rm -rf "$(ARTIFACTDIR)"
	mkdir -p "$(ARTIFACTDIR)/release"
	python3 .circleci/download_artifacts.py --prefix 'artifacts/release/' "$(BUILD_NUM)" "$(ARTIFACTDIR)/release"

##################### MULTI-ARCH HELPERS #####################

_run_make_foreach_target_tuple:
	if [ "$(RUN_MAKE_FOREACH_TARGET_TUPLE_ARG)" = "" ]; then \
		echo "RUN_MAKE_FOREACH_TARGET_TUPLE_ARG must be set"; \
		exit 1; \
	fi
	$(MAKE) $(RUN_MAKE_FOREACH_TARGET_TUPLE_ARG) GOOS=freebsd   GOARCH=amd64
	$(MAKE) $(RUN_MAKE_FOREACH_TARGET_TUPLE_ARG) GOOS=freebsd   GOARCH=386
	$(MAKE) $(RUN_MAKE_FOREACH_TARGET_TUPLE_ARG) GOOS=freebsd   GOARCH=arm	  GOARM=7
	$(MAKE) $(RUN_MAKE_FOREACH_TARGET_TUPLE_ARG) GOOS=freebsd   GOARCH=arm64
	$(MAKE) $(RUN_MAKE_FOREACH_TARGET_TUPLE_ARG) GOOS=linux     GOARCH=amd64
	$(MAKE) $(RUN_MAKE_FOREACH_TARGET_TUPLE_ARG) GOOS=linux     GOARCH=arm64
	$(MAKE) $(RUN_MAKE_FOREACH_TARGET_TUPLE_ARG) GOOS=linux     GOARCH=arm     GOARM=7
	$(MAKE) $(RUN_MAKE_FOREACH_TARGET_TUPLE_ARG) GOOS=linux     GOARCH=386
	$(MAKE) $(RUN_MAKE_FOREACH_TARGET_TUPLE_ARG) GOOS=darwin    GOARCH=amd64
	$(MAKE) $(RUN_MAKE_FOREACH_TARGET_TUPLE_ARG) GOOS=solaris   GOARCH=amd64
	$(MAKE) $(RUN_MAKE_FOREACH_TARGET_TUPLE_ARG) GOOS=illumos   GOARCH=amd64

##################### REGULAR TARGETS #####################
.PHONY: lint test-go test-platform cover-merge cover-html vet zrepl-bin test-platform-bin

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

generate:
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
	$(MAKE) -C docs \
		html \
		BUILDDIR=../artifacts/docs \
		SPHINXOPTS="-W --keep-going -n"

docs-clean:
	$(MAKE) -C docs \
		clean \
		BUILDDIR=../artifacts/docs
