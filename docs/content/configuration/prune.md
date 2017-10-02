+++
title = "Pruning Policies"
description = "Automated pruning of snapshots"
weight = 40
+++

In zrepl, *pruning* means *destroying snapshots by some policy*.

A *pruning policy* takes a list of snapshots and - for each snapshot - decides whether it should be kept or destroyed.

The job context defines which snapshots are even considered for pruning, for example through the `snapshot_prefix` variable.
Check the [job definition]({{< relref "configuration/jobs.md">}}) for details.

Currently, the retention grid is the only supported pruning policy.

## Retention Grid

```yaml
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
```

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
