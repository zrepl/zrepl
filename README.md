# zrepl
zrepl is a ZFS filesystem backup & replication solution written in Go.

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
    * Your distro's name and any versioning supplemental to zrepl's (e.g. package revesion) should be in this string
* Make sure you are informed about new zrepl versions, e.g. by subscribing to GitHub's release RSS feed.

## Developer Documentation

First, use `./lazy.sh devesetup` to install build dependencies and read `docs/installation.rst -> Compiling from Source`.

### Overall Architecture

The application architecture is documented as part of the user docs in the *Implementation* section (`docs/content/impl`).
Make sure to develop an understanding how zrepl is typically used by studying the user docs first.

### Project Structure

```
├── cmd
│   ├── sampleconf          # example configuration
├── docs                    # sphinx-based documentation
│   ├── **/*.rst            # documentation in reStructuredText
│   ├── sphinxconf
│   │   └── conf.py         # sphinx config (see commit 445a280 why its not in docs/)
│   ├── requirements.txt    # pip3 requirements to build documentation
│   ├── publish.sh          # shell script for automated rendering & deploy to zrepl.github.io repo
│   ├── public_git          # checkout of zrepl.github.io managed by above shell script
├── logger                  # logger package used by zrepl
├── rpc                     # rpc protocol implementation
├── sshbytestream           # io.ReadWriteCloser over SSH
├── util
└── zfs                     # ZFS wrappers, filesystemm diffing
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

