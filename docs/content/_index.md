+++
title = "zrepl - ZFS replication"
+++

# zrepl - ZFS replication

{{% notice info %}}
zrepl as well as this documentation is still under active development.
Use & test at your own risk ;)
{{% /notice %}}

## Getting started

The [5 minute tutorial setup](/tutorial/) gives you a first impression.

## Main Features

* Filesystem Replication
    * [x] Local & Remote
    * [x] Pull mode
    * [ ] Push mode
    * [x] Access control checks when pulling datasets
    * [x] [Flexible mapping]({{< ref "configuration/map_filter_syntax.md" >}}) rules
    * [x] Bookmarks support
    * [ ] Feature-negotiation for
        * Resumable `send & receive`
        * Compressed `send & receive`
        * Raw encrypted `send & receive` (as soon as it is available)
* Automatic snapshot creation
    * [x] Ensure fixed time interval between snapshots
* Automatic snapshot pruning
    * [x] <i class="fa fa-arrow-right" aria-hidden="true"></i> [Retention Grid]({{< ref "configuration/prune.md#retention-grid" >}})
* Maintainable implementation in Go
    * [x] Cross platform
    * [x] Type safe & testable code

## Contributing

zrepl is usable but nowhere near a stable release right now -  we are happy about contributors!

* Explore the codebase
    * These docs live in the `docs/` subdirectory
* Document any non-obvious / confusing / plain broken behavior you encounter when setting up zrepl for the first time
* Check the *Issues* and *Projects* sections for things to do

{{% panel header="<i class='fa fa-github'></i> Getting your code merged"%}}
[The <i class='fa fa-github'></i> GitHub repository](https://github.com/zrepl/zrepl) is where all development happens.
Open your issue / PR there.
{{% /panel %}}

