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
├── docs
│   ├── content             # hugo-based documentation -> sources for ./public_git
│   ├── deploy.sh           # shell script for automated rendering & deploy to zrepl.github.io repo
│   ├── public_git          # used by above shell script
│   └── themes
│       └── docdock         # submodule of our docdock theme fork
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
