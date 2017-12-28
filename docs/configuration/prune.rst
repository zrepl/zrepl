.. _prune:

Pruning Policies
================

In zrepl, *pruning* means *destroying snapshots by some policy*.

A *pruning policy* takes a list of snapshots and -- for each snapshot -- decides whether it should be kept or destroyed.

The job context defines which snapshots are even considered for pruning, for example through the ``snapshot_prefix`` variable.
Check the respective :ref:`job definition <job>` for details.

Currently, the :ref:`prune-retention-grid` is the only supported pruning policy.

.. _prune-retention-grid:

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
The ``grid`` field specifies a list of adjacent time intervals:
the left edge of the leftmost (first) interval is the ``creation`` date of the youngest snapshot.
All intervals to its right describe time intervals further in the past.

Each interval carries a maximum number of snapshots to keep.
It is secified via ``(keep=N)``, where ``N`` is either ``all`` (all snapshots are kept) or a positive integer.
The default value is **1**.

The following procedure happens during pruning:

#. The list of snapshots eligible for pruning is sorted by ``creation``
#. The left edge of the first interval is aligned to the ``creation`` date of the youngest snapshot
#. A list of buckets is created, one for each interval
#. The list of snapshots is split up into the buckets.
#. For each bucket

   #. the contained snapshot list is sorted by creation.
   #. snapshots from the list, oldest first, are destroyed until the specified ``keep`` count is reached.
   #. all remaining snapshots on the list are kept.

.. ATTENTION::

   The configuration of the first interval (``1x1h(keep=all)`` in the example) determines the **maximum allowable replication lag** because the source and destination pruning policies do not coordinate:
   if replication does not work for whatever reason, source will continue to execute the prune policy.
   Eventually, source destroys a snapshot that has never been replicated to destination, degrading the temporal resolution of your backup.

   Thus, **always** configure the first interval to ``1x?(keep=all)``, substituting ``?`` with the maximum time replication may fail due to downtimes, maintenance, connectivity issues, etc.

.. We intentionally do not mention that bookmarks are used to bridge the gap between source and dest that are out of sync snapshot-wise. This is an implementation detail.

