# zrepl
zrepl is a ZFS filesystem backup & replication solution written in Go.

## User Documentation

**User Documentation** cab be found at [zrepl.github.io](https://zrepl.github.io).

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

## Developer Documentation

### Overall Architecture

The application architecture is documented as part of the user docs in the *Implementation* section (`docs/content/impl`).
Make sure to develop an understanding how zrepl is typically used by studying the user docs first.

### Project Structure

```
├── cmd
│   ├── sampleconf          # example configuration
├── docs                    # sphinx-based documentation
│   ├── **/*.rst            # documentation in reStructuredText
│   ├── conf.py             # sphinx configuration
│   ├── publish.sh          # shell script for automated rendering & deploy to zrepl.github.io repo
│   ├── public_git          # checkout of zrepl.github.io used by above shell script
├── jobrun                  # OBSOLETE
├── logger                  # logger package used by zrepl
├── rpc                     # rpc protocol implementation
├── scratchpad              # small example programs demoing some internal packages. probably OBSOLETE
├── sshbytestream           # io.ReadWriteCloser over SSH
├── util
└── zfs                     # ZFS wrappers, filesystemm diffing
```

### Coding Workflow

* Open an issue when starting to hack on a new feature
* Commits should reference the issue they are related to
* Docs improvements not documenting new features do not require an issue.

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

### Building `docs`

```
cd docs
pip install sphinx sphinx-rtd-theme
make clean html
xdg-open _build/html/index.html
```
