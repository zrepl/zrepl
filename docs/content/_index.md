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
    * flexible mappings for filesystems
* automatic snapshot creation
    * periodic interval
* automatic snapshot pruning
    * *retention grid* TODO link explain
   
## Contributing

We are happy about contributors, both for the `zrepl` codebase and theses docs.
Feel free to open a ticket or even submit a pull request ;)

* <i class='fa fa-github'></i> [zrepl GitHub repository](https://github.com/zrepl/zrepl)
* these docs: 
    * were started on 2017-08-08 and are not committed yet. 
    * please use GitHub flavored markdown and open an issue in the `zrepl` repo
