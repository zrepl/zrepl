.. _prune:

Pruning Policies
================

In zrepl, *pruning* means *destroying filesystem versions by some policy* where filesystem versions are bookmarks and snapshots.

A *pruning policy* takes a list of filesystem versions and decides for each whether it should be kept or destroyed.

The job context defines which snapshots are even considered for pruning, for example through the ``snapshot_prefix`` variable.
Check the respective :ref:`job definition <job>` for details.

Currently, the :ref:`prune-retention-grid` is the only supported pruning policy.

.. TIP::

    You can perform a dry-run of a job's pruning policy using the ``zrepl test`` subcommand.

.. _prune-retention-grid:

Retention Grid
--------------

::

    jobs:
    - name: pull_app-srv
      type: pull
      ...
      prune:
        policy: grid
        grid: 1x1h(keep=all) | 24x1h | 35x1d | 6x30d
                │               │
                └─ one hour interval
                                │
                                └─ 24 adjacent one-hour intervals

    - name: pull_backup
      type: source
      interval: 10m
      prune:
        policy: grid
        grid: 1x1d(keep=all)
        keep_bookmarks: 144


The retention grid can be thought of as a time-based sieve:
The ``grid`` field specifies a list of adjacent time intervals:
the left edge of the leftmost (first) interval is the ``creation`` date of the youngest snapshot.
All intervals to its right describe time intervals further in the past.

Each interval carries a maximum number of snapshots to keep.
It is specified via ``(keep=N)``, where ``N`` is either ``all`` (all snapshots are kept) or a positive integer.
The default value is **1**.

Bookmarks are not affected by the above.
Instead, the ``keep_bookmarks`` field specifies the number of bookmarks to be kept per filesystem.
You only need to specify ``keep_bookmarks`` at the source-side of a replication setup since the destination side does not receive bookmarks.
You can specify ``all`` as a value to keep all bookmarks, but be warned that you should install some other way to prune unneeded ones then (see below).

The following procedure happens during pruning:

#. The list of snapshots eligible for pruning is sorted by ``creation``
#. The left edge of the first interval is aligned to the ``creation`` date of the youngest snapshot
#. A list of buckets is created, one for each interval
#. The list of snapshots is split up into the buckets.
#. For each bucket

   #. the contained snapshot list is sorted by creation.
   #. snapshots from the list, oldest first, are destroyed until the specified ``keep`` count is reached.
   #. all remaining snapshots on the list are kept.
#. The list of bookmarks eligible for pruning is sorted by ``createtxg`` and the most recent ``keep_bookmarks`` bookmarks are kept.

.. _replication-downtime:

.. ATTENTION::

    Be aware that ``keep_bookmarks x interval`` (interval of the job level) controls the **maximum allowable replication downtime** between source and destination.
    If replication does not work for whatever reason, source and destination will eventually run out of sync because the source will continue pruning snapshots.
    The only recovery in that case is full replication, which may not always be viable due to disk space or traffic constraints.

    Further note that while bookmarks consume a constant amount of disk space, listing them requires temporary dynamic **kernel memory** proportional to the number of bookmarks.
    Thus, do not use ``all`` or an inappropriately high value without good reason.

