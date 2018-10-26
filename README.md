[![GitHub license](https://img.shields.io/github/license/zrepl/zrepl.svg)](https://github.com/zrepl/zrepl/blob/master/LICENSE)
[![Language: Go](https://img.shields.io/badge/language-Go-6ad7e5.svg)](https://golang.org/)
[![User Docs](https://img.shields.io/badge/docs-web-blue.svg)](https://zrepl.github.io)
[![Donate via PayPal](https://img.shields.io/badge/donate-paypal-yellow.svg)](https://www.paypal.com/cgi-bin/webscr?cmd=_s-xclick&hosted_button_id=R5QSXJVYHGX96)
[![Donate via Liberapay](https://img.shields.io/badge/donate-liberapay-yellow.svg)](https://liberapay.com/zrepl/donate)
[![Twitter](https://img.shields.io/twitter/url/https/github.com/zrepl/zrepl.svg?style=social)](https://twitter.com/intent/tweet?text=Wow:&url=https%3A%2F%2Fgithub.com%2Fzrepl%2Fzrepl)

# zrepl
zrepl is a one-stop ZFS backup & replication solution.

## User Documentation

**User Documentation** can be found at [zrepl.github.io](https://zrepl.github.io).

## Bug Reports

1. If the issue is reproducible, enable debug logging, reproduce and capture the log.
2. Open an issue on GitHub, with logs pasted as GitHub gists / inline.

## Feature Requests

1. Does you feature request require default values / some kind of configuration?
   If so, think of an expressive configuration example.
2. Think of at least one use case that generalizes from your concrete application.
3. Open an issue on GitHub with example conf & use case attached.

The above does not apply if you already implemented everything.
Check out the *Coding Workflow* section below for details.

## Package Maintainer Information

* Follow the steps in `docs/installation.rst -> Compiling from Source` and read the Makefile / shell scripts used in this process.
* Make sure your distro is compatible with the paths in `docs/installation.rst`.
* Ship a default config that adheres to your distro's `hier` and logging system.
* Ship a service manager file and _please_ try to upstream it to this repository.
* Use `make release ZREPL_VERSION='mydistro-1.2.3_1'`
    * Your distro's name and any versioning supplemental to zrepl's (e.g. package revision) should be in this string
* Make sure you are informed about new zrepl versions, e.g. by subscribing to GitHub's release RSS feed.

## Developer Documentation

zrepl is written in [Go](https://golang.org) and uses [`dep`](https://github.com/golang/dep) to manage dependencies.
The documentation is written in [ReStructured Text](http://docutils.sourceforge.net/rst.html) using the [Sphinx](https://www.sphinx-doc.org) framework.

To get started, run `./lazy.sh devsetup` to easily install build dependencies and read `docs/installation.rst -> Compiling from Source`.

### Overall Architecture

The application architecture is documented as part of the user docs in the *Implementation* section (`docs/content/impl`).
Make sure to develop an understanding how zrepl is typically used by studying the user docs first.

### Project Structure

```
├── artifacts               # build artifcats generate by make
├── cli                     # wrapper around CLI package cobra
├── client                  # all subcommands that are not `daemon`
├── config                  # config data types (=> package yaml-config)
│   └── samples
├── daemon                  # the implementation of `zrepl daemon` subcommand
│   ├── filters
│   ├── job                 # job implementations
│   ├── logging             # logging outlets + formatters
│   ├── nethelpers
│   ├── prometheus
│   ├── pruner              # pruner implementation
│   ├── snapper             # snapshotter implementation
│   ├── streamrpcconfig     # abstraction for configuration of go-streamrpc
│   └── transport           # transports implementation
├── docs                    # sphinx-based documentation
│   ├── **/*.rst            # documentation in reStructuredText
│   ├── sphinxconf
│   │   └── conf.py         # sphinx config (see commit 445a280 why its not in docs/)
│   ├── requirements.txt    # pip3 requirements to build documentation
│   ├── publish.sh          # shell script for automated rendering & deploy to zrepl.github.io repo
│   └── public_git          # checkout of zrepl.github.io managed by above shell script
├── endpoint                # implementation of replication endpoints (=> package replication)
├── logger                  # our own logger package
├── pruning                 # pruning rules (the logic, not the actual execution)
│   └── retentiongrid
├── replication             # the fsm that implements replication of multiple file systems
│   ├── fsrep               # replication of a single filesystem
│   └── pdu                 # the protobuf-generated structs + helpers passed to an endpoint
├── tlsconf                 # abstraction for Go TLS server + client config
├── util
├── vendor                  # managed by dep
├── version                 # abstraction for versions (filled during build by Makefile)
└── zfs                     # zfs(8) wrappers
```

### Coding Workflow

* Open an issue when starting to hack on a new feature
* Commits should reference the issue they are related to
* Docs improvements not documenting new features do not require an issue.

### Breaking Changes

Backward-incompatible changes must be documented in the git commit message and are listed in `docs/changelog.rst`.

* Config-breaking changes must contain a line `BREAK CONFIG` in the commit message
* Other breaking changes must contain a line `BREAK` in the commit message

### Glossary & Naming Inconsistencies

In ZFS, *dataset* refers to the objects *filesystem*, *ZVOL* and *snapshot*. <br />
However, we need a word for *filesystem* & *ZVOL* but not a snapshot, bookmark, etc.

Toward the user, the following terminology is used:

* **filesystem**: a ZFS filesystem or a ZVOL
* **filesystem version**: a ZFS snapshot or a bookmark

Sadly, the zrepl implementation is inconsistent in its use of these words:
variables and types are often named *dataset* when they in fact refer to a *filesystem*.

There will not be a big refactoring (an attempt was made, but it's destroying too much history without much gain).

However, new contributions & patches should fix naming without further notice in the commit message.

