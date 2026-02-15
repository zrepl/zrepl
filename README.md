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

## Development

zrepl is written in [Go](https://golang.org) and uses [Go modules](https://github.com/golang/go/wiki/Modules) to manage dependencies.
The documentation is written in [ReStructured Text](http://docutils.sourceforge.net/rst.html) using the [Sphinx](https://www.sphinx-doc.org) framework.

### Building

#### Go Code

Dependencies:

* Go
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

Install [uv](https://docs.astral.sh/uv/getting-started/installation/), then run `make docs`.
uv automatically manages Python and dependencies.

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

* Prepare the release (as a PR to `master`):
  * Finalize `docs/changelog.rst` for the release.
  * Merge the PR. Docs are auto-published to zrepl.github.io on merge.
* Tag the release:
  * Git tag the release on the `master` branch (e.g., `vMAJOR.MINOR.0`).
    ```
    make tag-release ZREPL_TAG_VERSION=v0.7.0
    ```
  * Push the tag.
* Build and publish:
  * Run the `release` pipeline against the `master` branch (trigger via CircleCI UI).
    This URL: https://app.circleci.com/pipelines/github/zrepl/zrepl?branch=master.
    Example pipeline: https://app.circleci.com/pipelines/github/zrepl/zrepl/8547
  * Download artifacts using this handy makefile target.
    Note: `JOB_NUM` must be the **job number** from the `release-upload` job, **not the pipeline number**.
    Find it via: pipeline → `release` workflow → `release-upload` job number.
    Example URL: https://app.circleci.com/pipelines/github/zrepl/zrepl/8547/workflows/65feb2c9-15d7-46ab-a551-46d62a5769b0/jobs/66079/steps

    ```
    make download-circleci-release JOB_NUM=66079
    ```
  * Verify checksums and sign the checksums file:
    ```
    make verify-and-sign
    ```
  * Create GitHub release and upload artifacts:
    ```bash
    gh release create vX.Y.Z --title "vX.Y.Z" --notes "See changelog" --draft
    gh release upload vX.Y.Z artifacts/release/*
    ```
  * Review the draft release, edit the changelog, then publish.
  * Add the .rpm and .deb files to the official zrepl repos.
    * Code for management of these repos: https://github.com/zrepl/package-repo-ops (private repo at this time)
* Update docs version list:
  * Update `docs/_templates/versions.html` with the new release.
  * Verify the link to `zrepl-noarch.tar` in the GitHub release works.
  * Merge to `master` (docs auto-publish).

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


### Updating Dependencies

- Update the `go` directive and `toolchain` directive in `go.mod`
  - `go` is the minimum supported version
  - `toolchain` is the preferred toolchain version if `GOTOOLCHAIN` is not specified

Run `go mod tidy` to ensure consistency.

Update Go module dependencies:

```bash
# Update all other dependencies
go get -u -t ./...
go mod tidy

# Above might fail if there are version selection conflicts.
# Figure out what's going on by updating packages from error messages first.
# Example:
go get -u google.golang.org/genproto google.golang.org/grpc google.golang.org/protobuf
```

Update codegen & lint tools
- `protoc` =>  `build/get_protoc.bash`
  - GH releases publish sha256 sums
- `golangci-lint` => `build/get_golangci_lint.bash`
- bump versions in `build/tools.go`
  - we use the tools.go trick:
  - `go get -tags tools -u example.com/tool ; go mod tidy`
  - review whether we're ready to switch to `go tool`: https://github.com/zrepl/zrepl/pull/909

Now run `make generate`.

Run `make lint` and `make vet`.

Update the CI configuration `.circleci/config.yml`:
- Update Go version references (we reference the minimum and max supported version)
- Set `Makefile` `RELEASE_GOVERSION` to the new Go version
- Update the pinned ZFS release tags in the `platformtest` matrix (`zfs_release` parameter) to the latest patch releases of each OpenZFS branch (currently 2.2, 2.3, 2.4)
- Update the pinned Ubuntu machine image for `platformtest` (e.g. `ubuntu-2404:2025.09.1`) — this is pinned rather than `current` so the ZFS build cache stays valid across runs

Update docs build tooling:
- Update `uv` version in `.circleci/config.yml` (search for `astral.sh/uv/` and cache keys containing the version)
- Check if there's now a CircleCI orb for uv that we could use
- Update Python version in `docs/.python-version`

Update docs dependencies (Sphinx, sphinx-rtd-theme):
- Check current versions in `docs/pyproject.toml`
- Review upstream changelogs for breaking changes
- Update version constraints in `pyproject.toml` and the `uv` lockfile (see [uv docs on dependencies](https://docs.astral.sh/uv/concepts/projects/dependencies/)):
- Test locally with `make docs`

Kick a full CI pipeline run (`do_ci=true` and `do_release=true`).

Merge PR with merge commit.

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
