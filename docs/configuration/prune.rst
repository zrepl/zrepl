Snapshot Pruning
================

In zrepl, *pruning* means *destroying snapshots by some policy*.

A *pruning policy* takes a list of snapshots and - for each snapshot - decides whether it should be kept or destroyed.

The job context defines which snapshots are even considered for pruning, for example through the `snapshot_prefix` variable.
Check the [job definition]({{< relref "configuration/jobs.md">}}) for details.

Currently, the retention grid is the only supported pruning policy.

Retention Grid
--------------

::

    jobs:
    - name: pull_app-srv
      ...
      prune:
        policy: grid
        grid: 1x1h(keep=all) | 24x1h | 35x1d | 6x30d
                │               │
                └─ one hour interval
                                │
                                └─ 24 adjacent one-hour intervals

The retention grid can be thought of as a time-based sieve:

The `grid` field specifies a list of adjacent time intervals:
the left edge of the leftmost (first) interval is the `creation` date of the youngest snapshot.
All intervals to its right describe time intervals further in the past.

Each interval carries a maximum number of snapshots to keep.
It is secified via `(keep=N)`, where `N` is either `all` (all snapshots are kept) or a positive integer.
The default value is **1**.

The following procedure happens during pruning:

1. The list of snapshots eligible for pruning is sorted by `creation`
1. The left edge of the first interval is aligned to the `creation` date of the youngest snapshot
1. A list of buckets is created, one for each interval
1. The list of snapshots is split up into the buckets.
1. For each bucket

   1. the contained snapshot list is sorted by creation.
   1. snapshots from the list, oldest first, are destroyed until the specified `keep` count is reached.
   1. all remaining snapshots on the list are kept.

.. ATTENTION::

    The configuration of the first interval (`1x1h(keep=all)` in the example) determines the **maximum allowable replication lag** between source and destination.
    After the first interval, source and destination likely have different retention settings.
    This means source and destination may prune different snapshots, prohibiting incremental replication froms snapshots that are not in the first interval.

    **Always** configure the first interval to **`1x?(keep=all)`**, substituting `?` with the maximum time replication may fail due to downtimes, maintenance, connectivity issues, etc.
    After outages longer than `?` you may be required to perform **full replication** again.

