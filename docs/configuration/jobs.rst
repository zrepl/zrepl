Job Types
=========

A *job* is the unit of activity tracked by the zrepl daemon and configured in the [configuration file]({{< relref "install/_index.md#configuration-files" >}}).

Every job has a unique `name`, a `type` and type-dependent fields which are documented on this page.

Check out the [tutorial]({{< relref "tutorial/_index.md" >}}) and {{< sampleconflink >}} for examples on how job types are actually used.

.. ATTENTION::

    Currently, zrepl does not replicate filesystem properties.
    Whe receiving a filesystem, it is never mounted (`-u` flag)  and `mountpoint=none` is set.
    This is temporary and being worked on {{< zrepl-issue 24 >}}.

Source Job
----------

========== ======= =====================
Parameter  Default Description / Example
========== ======= =====================
========== ======= =====================

::

    |-----|-------|-------|
    |`type`||`source`|
    |`name`||unique name of the job|
    |`serve`||{{< zrepl-transport "serve transport" >}} specification|
    |`datasets`||{{< zrepl-filter >}} for datasets to expose to client|
    |`snapshot_prefix`||prefix for ZFS snapshots taken by this job|
    |`interval`||snapshotting interval|
    |`prune`||{{< zrepl-prune >}} policy for datasets in `datasets` with prefix `snapshot_prefix`|

* Snapshotting Task (every `interval`, {{% zrepl-job-patient %}})
    1. A snapshot of filesystems matched by `datasets` is taken every `interval` with prefix `snapshot_prefix`.
    1. The `prune` policy is triggered on datasets matched by `datasets` with snapshots matched by `snapshot_prefix`.
* Serve Task
    * Wait for connections from pull job using `serve`

A source job is the counterpart to a [pull job]({{< relref "#pull" >}}).

Note that the prune policy determines the maximum replication lag:
a pull job may stop replication due to link failure, misconfiguration or administrative action.
The source prune policy will eventually destroy the last common snapshot between source and pull job, requiring full replication.
Make sure you read the [prune policy documentation]({{< relref "configuration/prune.md" >}}).

Example: {{< sampleconflink "pullbackup/productionhost.yml" >}}

Pull Job
--------

::

    |Parameter|Default|Description / Example|
    |-----|-------|-------|
    |`type`||`pull`|
    |`name`||unique name of the job|
    |`connect`||{{< zrepl-transport "connect transport" >}} specification|
    |`interval`||Interval between pull attempts|
    |`mapping`||{{< zrepl-mapping >}} for remote to local filesystems|
    |`initial_repl_policy`|`most_recent`|initial replication policy|
    |`snapshot_prefix`||prefix filter used for replication & pruning|
    |`prune`||{{< zrepl-prune >}} policy for local filesystems reachable by `mapping`|

    - Main Task (every `interval`, {{% zrepl-job-patient %}})
      #. A connection to the remote source job is established using the strategy in `connect`
      #. `mapping` maps filesystems presented by the remote side to local *target filesystems*
      #. Those remote filesystems with a local *target filesystem* are replicated
         #. Only snapshots with prefix `snapshot_prefix` are replicated.
         #. If possible, incremental replication takes place.
         #. If the local target filesystem does not exist, `initial_repl_policy` is used.
         #. On conflicts, an error is logged but replication of other filesystems with mapping continues.
      #. The `prune` policy is triggered for all *target filesystems*

A pull job is the counterpart to a [source job]({{< relref "#source" >}}).

Example: {{< sampleconflink "pullbackup/backuphost.yml" >}}

Local Job
---------

::

    |Parameter|Default|Description / Example|
    |-----|-------|-------|
    |`type`||`local`|
    |`name`||unique name of the job|
    |`mapping`||{{<zrepl-mapping>}} from source to target filesystem (both local)|
    |`snapshot_prefix`||prefix for ZFS snapshots taken by this job|
    |`interval`|snapshotting & replication interval|
    |`initial_repl_policy`|`most_recent`|initial replication policy|
    |`prune_lhs`||pruning policy on left-hand-side (source)|
    |`prune_rhs`||pruning policy on right-hand-side (target)|

    * Main Task (every `interval`, {{% zrepl-job-patient %}})
        1. Evaluate `mapping` for local filesystems, those with a *target filesystem* are called *mapped filesystems*.
        1. Snapshot *mapped filesystems* with `snapshot_prefix`.
        1. Replicate *mapped filesystems* to their respective *target filesystems*:
            1. Only snapshots with prefix `snapshot_prefix` are replicated.
            1. If possible, incremental replication takes place.
            1. If the *target filesystem* does not exist, `initial_repl_policy` is used.
            1. On conflicts, an error is logged but replication of other *mapped filesystems* continues.
        1. The `prune_lhs` policy is triggered for all *mapped filesystems*
        1. The `prune_rhs` policy is triggered for all *target filesystems*

A local job is combination of source & pull job executed on the same machine.

Example: {{< sampleconflink "localbackup/host1.yml" >}}

Terminology
-----------

task

    A job consists of one or more tasks and a task consists of one or more steps.
    Some tasks may be periodic while others wait for an event to occur.

patient task

    A patient task is supposed to execute some task every `interval`.
    We call the start of the task an *invocation*.

    * If the task completes in less than `interval`, the task is restarted at `last_invocation + interval`.
    * Otherwise, a patient job
        * logs a warning as soon as a task exceeds its configured `interval`
        * waits for the last invocation to finish
        * logs a warning with the effective task duration
        * immediately starts a new invocation of the task
