.PHONY: generate build test vet cover release docs docs-clean clean release-bins
.DEFAULT_GOAL := build

ROOT := github.com/zrepl/zrepl
SUBPKGS := cmd logger rpc sshbytestream util

_TESTPKGS := $(ROOT) $(foreach p,$(SUBPKGS),$(ROOT)/$(p))

ARTIFACTDIR := artifacts

generate: #not part of the build, must do that manually
	@for pkg in $(_TESTPKGS); do\
		go generate "$$pkg" || exit 1; \
	done;

build:
		go build -o $(ARTIFACTDIR)/zrepl

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
	GOOS=linux GOARCH=amd64   go build -o "$(ARTIFACTDIR)/zrepl-linux-amd64"
	GOOS=freebsd GOARCH=amd64 go build -o "$(ARTIFACTDIR)/zrepl-freebsd-amd64"

release: release-bins docs

clean: docs-clean
	rm -rf "$(ARTIFACTDIR)"
