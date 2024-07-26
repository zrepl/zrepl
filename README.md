[![GitHub license](https://img.shields.io/github/license/zrepl/zrepl.svg)](https://github.com/zrepl/zrepl/blob/master/LICENSE)
[![Language: Go](https://img.shields.io/badge/lang-Go-6ad7e5.svg)](https://golang.org/)
[![User Docs](https://img.shields.io/badge/docs-web-blue.svg)](https://zrepl.github.io)
[![Support me on Patreon](https://img.shields.io/badge/dynamic/json?color=yellow&label=Patreon&query=data.attributes.patron_count&url=https%3A%2F%2Fwww.patreon.com%2Fapi%2Fcampaigns%2F3095079)](https://patreon.com/zrepl)
[![Donate via GitHub Sponsors](https://img.shields.io/static/v1?label=Sponsor&message=%E2%9D%A4&logo=GitHub&style=flat&color=yellow)](https://github.com/sponsors/problame)
[![Donate via Liberapay](https://img.shields.io/liberapay/patrons/zrepl.svg?logo=liberapay)](https://liberapay.com/zrepl/donate)
[![Donate via PayPal](https://img.shields.io/badge/donate-paypal-yellow.svg)](https://www.paypal.com/cgi-bin/webscr?cmd=_s-xclick&hosted_button_id=R5QSXJVYHGX96)
[![Twitter](https://img.shields.io/twitter/url/https/github.com/zrepl/zrepl.svg?style=social)](https://twitter.com/intent/tweet?text=Wow:&url=https%3A%2F%2Fgithub.com%2Fzrepl%2Fzrepl)
[![Chat](https://img.shields.io/badge/chat-matrix-blue.svg)](https://matrix.to/#/#zrepl:matrix.org)

# zrepl
zrepl is a one-stop ZFS backup & replication solution.

## User Documentation

**User Documentation** can be found at [zrepl.github.io](https://zrepl.github.io).

## Bug Reports

1. If the issue is reproducible, enable debug logging, reproduce and capture the log.
2. Open an issue on GitHub, with logs pasted as GitHub gists / inline.

## Feature Requests

1. Does your feature request require default values / some kind of configuration?
   If so, think of an expressive configuration example.
2. Think of at least one use case that generalizes from your concrete application.
3. Open an issue on GitHub with example conf & use case attached.
4. **Optional**: [Contact Christian Schwarz](https://cschwarz.com) for contract work.

The above does not apply if you already implemented everything.
Check out the *Coding Workflow* section below for details.

## Building, Releasing, Downstream-Packaging

This section provides an overview of the zrepl build & release process.
Check out `docs/installation/compile-from-source.rst` for build-from-source instructions.

### Overview

zrepl is written in [Go](https://golang.org) and uses [Go modules](https://github.com/golang/go/wiki/Modules) to manage dependencies.
The documentation is written in [ReStructured Text](http://docutils.sourceforge.net/rst.html) using the [Sphinx](https://www.sphinx-doc.org) framework.

Install **build dependencies** using  `./lazy.sh devsetup`.
`lazy.sh` uses `python3-pip` to fetch the build dependencies for the docs - you might want to use a [venv](https://docs.python.org/3/library/venv.html).
If you just want to install the Go dependencies, run `./lazy.sh godep`.

The **test suite** is split into pure **Go tests** (`make test-go`) and **platform tests** that interact with ZFS and thus generally **require root privileges** (`sudo make test-platform`).
Platform tests run on their own pool with the name `zreplplatformtest`, which is created using the file vdev in `/tmp`.

For a full **code coverage** profile, run `make test-go COVER=1 && sudo make test-platform && make cover-merge`.
An HTML report can be generated using `make cover-html`.

**Code generation** is triggered by `make generate`. Generated code is committed to the source tree.

### Build & Release Process

**The `Makefile` is catering to the needs of developers & CI, not distro packagers**.
It provides phony targets for
* local development (building, running tests, etc)
* building a release in Docker (used by the CI & release management)
* building .deb and .rpm packages out of the release artifacts.

**Build tooling & dependencies** are documented as code in `lazy.sh`.
Go dependencies are then fetched by the go command and pip dependencies are pinned through a `requirements.txt`.

**We use CircleCI for continuous integration**.
There are two workflows:

* `ci` runs for every commit / branch / tag pushed to GitHub.
  It is supposed to run very fast (<5min and provides quick feedback to developers).
  It runs formatting checks, lints and tests on the most important OSes / architectures.
  Artifacts are published to minio.cschwarz.com (see GitHub Commit Status).

* `release` runs
  * on manual triggers through the CircleCI API (in order to produce a release)
  * periodically on `master`
  Artifacts are published to minio.cschwarz.com (see GitHub Commit Status).

**Releases** are issued via Git tags + GitHub Releases feature.
The procedure to issue a release is as follows:
* Issue the source release:
  * Git tag the release on the `master` branch.
  * Push the tag.
  * Run `./docs/publish.sh` to re-build & push zrepl.github.io.
* Issue the official binary release:
  * Run the `release` pipeline (triggered via CircleCI API)
  * Download the artifacts to the release manager's machine.
  * Create a GitHub release, edit the changelog, upload all the release artifacts, including .rpm and .deb files.
  * Issue the GitHub release.
  * Add the .rpm and .deb files to the official zrepl repos, publish those.

**Official binary releases are not re-built when Go receives an update. If the Go update is critical to zrepl (e.g. a Go security update that affects zrepl), we'd issue a new source release**.
The rationale for this is that whereas distros provide a mechanism for this (`$zrepl_source_release-$distro_package_revision`), GitHub Releases doesn't which means we'd need to update the existing GitHub release's assets, which nobody would notice (no RSS feed updates, etc.).
Downstream packagers can read the changelog to determine whether they want to push that minor release into their distro or simply skip it.

### Additional Notes to Distro Package Maintainers

* Run the platform tests (Docs -> Usage -> Platform Tests) **on a test system** to validate that zrepl's abstractions on top of ZFS work with the system ZFS.
* Ship a default config that adheres to your distro's `hier` and logging system.
* Ship a service manager file and _please_ try to upstream it to this repository.
  * `dist/systemd` contains a Systemd unit template.
* Ship other material provided in `./dist`, e.g. in `/usr/share/zrepl/`.
* Have a look at the `Makefile`'s `ZREPL_VERSION` variable and how it passed to Go's `ldFlags`.
  This is how `zrepl version` knows what version number to show.
  Your build system should set the `ldFlags` flags appropriately and add a prefix or suffix that indicates that the given zrepl binary is a distro build, not an official one.
* Make sure you are informed about new zrepl versions, e.g. by subscribing to GitHub's release RSS feed.


## Contributing Code

* Open an issue when starting to hack on a new feature
* Commits should reference the issue they are related to
* Docs improvements not documenting new features do not require an issue.

### Breaking Changes

Backward-incompatible changes must be documented in the git commit message and are listed in `docs/changelog.rst`.

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
