[![GitHub license](https://img.shields.io/github/license/zrepl/zrepl.svg)](https://github.com/LyingCak3/zrepl/blob/master/LICENSE)
[![Language: Go](https://img.shields.io/badge/lang-Go-6ad7e5.svg)](https://golang.org/)
[![User Docs](https://img.shields.io/badge/docs-web-blue.svg)](https://zrepl.github.io)s
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

The above does not apply if you already implemented everything.
Check out the *Coding Workflow* section below for details.

## Development

zrepl is written in [Go](https://golang.org) and uses [Go modules](https://github.com/golang/go/wiki/Modules) to manage dependencies.
The documentation is written in [ReStructured Text](http://docutils.sourceforge.net/rst.html) using the [Sphinx](https://www.sphinx-doc.org) framework.

### Building

#### Go Code

Dependencies:

* Go 1.22 or newer
* GNU Make
* Git
* wget (`make generate`)
* unzip (`make generate`)

Some Go code is **generated**, and generated code is committed to the source tree.
Therefore, building does not require having code generation tools set up.
When making changes that require code to be (re-)generated, run `make generate`.
I downloads and installs pinned versions of the code generation tools into `./build/install`.
There is a CI check that ensures Git state is clean, i.e., code generation has been done by a PR and is deterministic.

#### Docs

Set up a Python environment that has `docs/requirements.txt` installed via `pip`.
Use a  [venv](https://docs.python.org/3/library/venv.html) to avoid global state.

### Testing

The **test suite** is split into pure **Go tests** (`make test-go`) and **platform tests** that interact with ZFS and thus generally **require root privileges** (`sudo make test-platform`).
Platform tests run on their own pool with the name `zreplplatformtest`, which is created using the file vdev in `/tmp`.

For a full **code coverage** profile, run `make test-go COVER=1 && sudo make test-platform && make cover-merge`.
An HTML report can be generated using `make cover-html`.

### Circle CI

We use CircleCI for automated build & test pre- and post-merge.

There are two workflows:

* `ci` runs for every commit / branch / tag pushed to GitHub.
  It is supposed to run very fast (<5min and provides quick feedback to developers).
  It runs formatting checks, lints and tests on the most important OSes / architectures.

* `release` runs
  * on manual triggers through the CircleCI API (in order to produce a release)
  * periodically on `master`

Artifacts are stored in CircleCI.

### Releasing

All zrepl releases are git-tagged and then published as a GitHub Release.
There is a git tag for each zrepl release, usually `vMAJOR.MINOR.0`.
We don't move git tags once the release has been published.

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
  * Add the .rpm and .deb files to the official zrepl repos.
    * Code for management of these repos: https://github.com/zrepl/package-repo-ops (private repo at this time)

#### Patch releases, Go toolchain updates, APT/RPM Package rebuilds

`vMAJOR.MINOR.0` is typically a tagged commit on `master`, because development velocity isn't high
and thus release branches for stabilization aren't necessary.

Occasionally though there is a need for patch changes to a release, e.g.
- security issue in a dependency
- Go toolchain update (e.g. security issue in standard library)

The procedure for this is the following
- create a branch off the release tag we need to patch, named `releases/MAJOR.MINOR.X`
- that branch will never be merged into `master`, it'll be a dead-end for this specific patch
- make changes in that branch
- make the final commit that bumps version numbers
- create the git tag
- follow general procedure for publishing the release (previous sectino

For Go toolchain updates and package rebuilds with no source code changes, leverage the APT/RPM package revision field.
Control via the `ZREPL_PACKAGE_RELEASE` Makefile variable, see `origin/releases/0.6.1-2` for an example.



## Notes to Distro Package Maintainers

* The `Makefile` in this project is not suitable for builds in distros.
* Run the platform tests (Docs -> Usage -> Platform Tests) **on a test system** to validate that zrepl's abstractions on top of ZFS work with the system ZFS.
* Ship a default config that adheres to your distro's `hier` and logging system.
* Ship a service manager file and _please_ try to upstream it to this repository.
  * `dist/systemd` contains a Systemd unit template.
* Ship other material provided in `./dist`, e.g. in `/usr/share/zrepl/`.
* Have a look at the `Makefile`'s `ZREPL_VERSION` variable and how it passed to Go's `ldFlags`.
  This is how `zrepl version` knows what version number to show.
  Your build system should set the `ldFlags` flags appropriately and add a prefix or suffix that indicates that the given zrepl binary is a distro build, not an official one.
* Make sure you are informed about new zrepl versions, e.g. by subscribing to GitHub's release RSS feed.
