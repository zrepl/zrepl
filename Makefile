.PHONY: generate build test vet cover release clean

ROOT := github.com/zrepl/zrepl
SUBPKGS := cmd logger rpc sshbytestream util

_TESTPKGS := $(ROOT) $(foreach p,$(SUBPKGS),$(ROOT)/$(p))

ARTIFACTDIR := artifacts

build: generate
		go build -o $(ARTIFACTDIR)/zrepl

generate:
	@for pkg in $(_TESTPKGS); do\
		go generate "$$pkg" || exit 1; \
	done;

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

artifacts:
	mkdir artifacts

release: generate artifacts vet test
	GOOS=linux GOARCH=amd64   go build -o "$(ARTIFACTDIR)/zrepl-linux-amd64"
	GOOS=freebsd GOARCH=amd64 go build -o "$(ARTIFACTDIR)/zrepl-freebsd-amd64"

clean:
	rm -rf "$(ARTIFACTDIR)"
