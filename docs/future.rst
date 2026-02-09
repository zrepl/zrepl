.. include:: global.rst.inc

.. _future:

Future
======

This page contains some notes about future plans for zrepl.
Then again, Christian only has very limited time available for zrepl maintenance these days.
So, don't count on any of this happening in the near future.

One big development that has happened in recent years is this fork of zrepl: https://github.com/dsh2dsh/zrepl
We should figure out whether we can pull in features from there.

The next major step in terms of new feature development for zrepl would be to revise snapshot management:

- Make it easy to decouple snapshot management (snapshotting, pruning) from replication.
- Ability to include/exclude snapshots from replication.
  This is useful for aforementioned decoupling, e.g., separate snapshot prefixes for local & remote replication.
  Also, it makes explicit that by default, zrepl replicates all snapshots, and that
  replication has no concept of "zrepl-created snapshots", which is a common misconception.
- Use of ``zfs snapshot`` comma syntax or channel programs to take snapshots of multiple
  datasets atomically.
- Provide an alternative to the ``grid`` pruning policy.
  Most likely something based on hourly/daily/weekly/monthly "trains" plus a count.
- Ability to prune at the granularity of the **group** of snapshots created at a given
  time, as opposed to the individual snapshots within a dataset.
  Maybe this will be addressed by the alternative to the ``grid`` pruning policy,
  as it will likely be more predictable.

Those changes will likely come with some breakage in the config.
However, I want to avoid breaking **use cases** that are satisfied by the current design.
There will be beta/RC releases to give users a chance to evaluate.
