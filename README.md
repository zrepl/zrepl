[![GitHub license](https://img.shields.io/github/license/zrepl/zrepl.svg)](https://github.com/zrepl/zrepl/blob/master/LICENSE)
[![Language: Go](https://img.shields.io/badge/language-Go-6ad7e5.svg)](https://golang.org/)
[![User Docs](https://img.shields.io/badge/docs-web-blue.svg)](https://zrepl.github.io)
[![Donate via Patreon](https://img.shields.io/endpoint.svg?url=https%3A%2F%2Fshieldsio-patreon.herokuapp.com%2Fzrepl%2Fpledges&style=flat&color=yellow)](https://www.patreon.com/zrepl)
[![Donate via Liberapay](https://img.shields.io/liberapay/receives/zrepl.svg?logo=liberapay)](https://liberapay.com/zrepl/donate)
[![Donate via PayPal](https://img.shields.io/badge/donate-paypal-yellow.svg)](https://www.paypal.com/cgi-bin/webscr?cmd=_s-xclick&hosted_button_id=R5QSXJVYHGX96)
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
4. **Optional**: [Post a bounty](https://www.bountysource.com/teams/zrepl) on the issue, or [contact Christian Schwarz](https://cschwarz.com) for contract work.

The above does not apply if you already implemented everything.
Check out the *Coding Workflow* section below for details.

## Package Maintainer Information

* Follow the steps in `docs/installation.rst -> Compiling from Source` and read the Makefile / shell scripts used in this process.
* Make sure your distro is compatible with the paths in `docs/installation.rst`.
* Ship a default config that adheres to your distro's `hier` and logging system.
* Ship a service manager file and _please_ try to upstream it to this repository.
  * `dist/systemd` contains a Systemd unit template.
* Ship other material provided in `./dist`, e.g. in `/usr/share/zrepl/`.
* Use `make release ZREPL_VERSION='mydistro-1.2.3_1'`
    * Your distro's name and any versioning supplemental to zrepl's (e.g. package revision) should be in this string
* Use `make platformtest` **on a test system** to validate that zrepl's abstractions on top of ZFS work with the system ZFS.
* Make sure you are informed about new zrepl versions, e.g. by subscribing to GitHub's release RSS feed.

## Developer Documentation

zrepl is written in [Go](https://golang.org) and uses [Go modules](https://github.com/golang/go/wiki/Modules) to manage dependencies.
The documentation is written in [ReStructured Text](http://docutils.sourceforge.net/rst.html) using the [Sphinx](https://www.sphinx-doc.org) framework.

To get started, run `./lazy.sh devsetup` to easily install build dependencies and read `docs/installation.rst -> Compiling from Source`.
`lazy.sh` uses `python3-pip` to fetch the build dependencies for the docs - you might want to use a [venv](https://docs.python.org/3/library/venv.html).
If you just want to install the Go dependencies, run `./lazy.sh godep`.

### Project Structure

```
├── artifacts               # build artifcats generate by make
├── cli                     # wrapper around CLI package cobra
├── client                  # all subcommands that are not `daemon`
├── config                  # config data types (=> package yaml-config)
│   └── samples
├── daemon                  # the implementation of `zrepl daemon` subcommand
│   ├── filters
│   ├── hooks               # snapshot hooks
│   ├── job                 # job implementations
│   ├── logging             # logging outlets + formatters
│   ├── nethelpers
│   ├── prometheus
│   ├── pruner              # pruner implementation
│   ├── snapper             # snapshotter implementation
├── dist                    # supplemental material for users & package maintainers
├── docs                    # sphinx-based documentation
│   ├── **/*.rst            # documentation in reStructuredText
│   ├── sphinxconf
│   │   └── conf.py         # sphinx config (see commit 445a280 why its not in docs/)
│   ├── requirements.txt    # pip3 requirements to build documentation
│   ├── publish.sh          # shell script for automated rendering & deploy to zrepl.github.io repo
│   └── public_git          # checkout of zrepl.github.io managed by above shell script
├── endpoint                # implementation of replication endpoints (=> package replication)
├── logger                  # our own logger package
├── platformtest            # test suite for our zfs abstractions (error classification, etc)
├── pruning                 # pruning rules (the logic, not the actual execution)
│   └── retentiongrid
├── replication
│   ├── driver              # the driver of the replication logic (status reporting, error handling)
│   ├── logic               # planning & executing replication steps via rpc
|   |   └── pdu             # the generated gRPC & protobuf code used in replication (and endpoints)
│   └── report              # the JSON-serializable report datastructures exposed to the client
├── rpc                     # the hybrid gRPC + ./dataconn RPC client: connects to a remote replication.Endpoint
│   ├── dataconn            # Bulk data-transfer RPC protocol
│   ├── grpcclientidentity  # adaptor to inject package transport's 'client identity' concept into gRPC contexts
│   ├── netadaptor          # adaptor to convert a package transport's Connecter and Listener into net.* primitives
│   ├── transportmux        # TCP connecter and listener used to split control & data traffic
│   └── versionhandshake    # replication protocol version handshake perfomed on newly established connections
├── tlsconf                 # abstraction for Go TLS server + client config
├── transport               # transport implementations
│   ├── fromconfig
│   ├── local
│   ├── ssh
│   ├── tcp
│   └── tls
├── util
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

### RPC debugging

Optionally, there are various RPC-related environment variables, that if set to something != `""` will produce additional debug output on stderr:

https://github.com/zrepl/zrepl/blob/master/rpc/rpc_debug.go#L11

https://github.com/zrepl/zrepl/blob/master/rpc/dataconn/dataconn_debug.go#L11

https://github.com/zrepl/zrepl/blob/master/rpc/dataconn/stream/stream_debug.go#L11

https://github.com/zrepl/zrepl/blob/master/rpc/dataconn/heartbeatconn/heartbeatconn_debug.go#L11

