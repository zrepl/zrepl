+++
title = "zrepl - ZFS replication"
+++

# zrepl - ZFS replication

`zrepl` is a tool for replicating ZFS filesystems.

{{% panel theme="danger" header="Important" %}}
`zrepl` as well as this documentation is still under active
development. Some of the features below are not implemented yet. Use & test at your own risk ;)
{{% / panel  %}}

## Main Features

* filesystem replication
    * local & over network (SSH)
    * push & pull mode
    * snapshots & bookmarks support
    * feature-negotiation for
        * resumable `send & receive`
        * compressed `send & receive`
        * raw encrypted `send & receive` (as soon as it is available)
    * access control checks when pulling datasets
    * [flexible mappings]({{< ref "configuration/overview.md#mapping-filter-syntax" >}}) for filesystems
* automatic snapshot creation
    * periodic interval
* automatic snapshot pruning
    * [Retention Grid]({{< ref "configuration/snapshots.md#retention-grid" >}})
   
## Contributing

`zrepl` is usable but nowhere near a stable release right now -  we are happy
about contributors!

* Explore the codebase
    * These docs live in the `docs/` subdirectory
* Document non-obvious / confusing / plain broken things you encounter when using `zrepl` for the first time
* Check the *Issues* and *Projects* sections for things to do ;)

{{% panel header="<i class='fa fa-github'></i> Getting your code merged"%}}
[The <i class='fa fa-github'></i> GitHub repository](https://github.com/zrepl/zrepl) is where all development happens.
Open your issue / PR there.
{{% /panel %}}

