.. _prune:

Pruning Policies
================

In zrepl, *pruning* means *destroying snapshots*.
Pruning must happen on both sides of a replication or the systems would inevitable run out of disk space at some point.

Typically, the requirements to temporal resolution and maximum retention time differ per side.
For example, when using zrepl to back up a busy database server, you will want high temporal resolution (snapshots every 10 min) for the last 24h in case of administrative disasters, but cannot afford to store them for much longer because you might have high turnover volume in the database.
On the receiving side, you may have more disk space available, or need to comply with other backup retention policies.

zrepl uses a set of  **keep rules** to determine which snapshots shall be kept per filesystem.
**A snapshot that is not kept by any rule is destroyed.**
The keep rules are **evaluated on the active side** (:ref:`push <job-push>` or :ref:`pull job <job-pull>`) of the replication setup, for both active and passive side, after replication completed or was determined to have failed permanently.

Example Configuration:

::

   jobs:
     - type: push
       name: ...
       connect: ...
       filesystems: {
         "<": true,
         "tmp": false
       }
       snapshotting:
         type: periodic
         prefix: zrepl_
         interval: 10m
       pruning:
         keep_sender:
           - type: not_replicated
           # make sure manually created snapshots by the administrator are kept
           - type: regex
             regex: "^manual_.*"
           - type: grid
             grid: 1x1h(keep=all) | 24x1h | 14x1d
             regex: "^zrepl_.*"
         keep_receiver:
           - type: grid
             grid: 1x1h(keep=all) | 24x1h | 35x1d | 6x30d
             regex: "^zrepl_.*"
           # manually created snapshots will be kept forever on receiver

.. DANGER::
    You might have **existing snapshots** of filesystems affected by pruning which you want to keep, i.e. not be destroyed by zrepl.
    Make sure to actually add the necessary ``regex`` keep rules on both sides, like with ``manual`` in the example above.

.. ATTENTION::

    It is currently not possible to define pruning on a source job.
    The source job creates snapshots, which means that extended replication downtime will fill up the source's zpool with snapshots, since pruning is directed by the corresponding active side (pull job).
    If this is a potential risk for you, consider using :ref:`push mode <job-push>`.


.. _prune-keep-not-replicated:

Policy ``not_replicated``
-------------------------

::

   jobs:
   - type: push
     pruning:
       keep_sender:
       - type: not_replicated
     ...

``not_replicated`` keeps all snapshots that have not been replicated to the receiving side.
It only makes sense to specify this rule on a sender (source or push job).
The state required to evaluate this rule is stored in the :ref:`replication cursor bookmark <replication-cursor-bookmark>` on the sending side.

.. _prune-keep-retention-grid:

Policy ``grid``
---------------

::

    jobs:
    - type: pull
      pruning:
        keep_receiver:
        - type: grid
          regex: "^zrepl_.*"
          grid: 1x1h(keep=all) | 24x1h | 35x1d | 6x30d
                │                │
                └─ one hour interval
                                 │
                                 └─ 24 adjacent one-hour intervals
      ...

The retention grid can be thought of as a time-based sieve:
The ``grid`` field specifies a list of adjacent time intervals:
the left edge of the leftmost (first) interval is the ``creation`` date of the youngest snapshot.
All intervals to its right describe time intervals further in the past.

Each interval carries a maximum number of snapshots to keep.
It is specified via ``(keep=N)``, where ``N`` is either ``all`` (all snapshots are kept) or a positive integer.
The default value is **keep=1**.

The following procedure happens during pruning:

#. The list of snapshots is filtered by the regular expression in ``regex``.
   Only snapshots names that match the regex are considered for this rule, all others are not affected.
#. The filtered list of snapshots is sorted by ``creation``
#. The left edge of the first interval is aligned to the ``creation`` date of the youngest snapshot
#. A list of buckets is created, one for each interval
#. The list of snapshots is split up into the buckets.
#. For each bucket

   #. the contained snapshot list is sorted by creation.
   #. snapshots from the list, oldest first, are destroyed until the specified ``keep`` count is reached.
   #. all remaining snapshots on the list are kept.


.. _prune-keep-last-n:

Policy ``last_n``
-----------------

::

   jobs:
     - type: push
       pruning:
         keep_receiver:
         - type: last_n
           count: 10
     ...

``last_n`` keeps the last ``count`` snapshots (last = youngest = most recent creation date).

.. _prune-keep-regex:

Policy ``regex``
----------------

::

   jobs:
     - type: push
       pruning:
         keep_receiver:
         # keep all snapshots with prefix zrepl_ or manual_
         - type: regex
           regex: "^(zrepl|manual)_.*"

     - type: push
       snapshotting:
         prefix: zrepl_
       pruning:
         keep_sender:
         # keep all snapshots that were not created by zrepl
         - type: regex
           negate: true
           regex: "^zrepl_.*"

``regex`` keeps all snapshots whose names are matched by the regular expressionin ``regex``.
Like all other regular expression fields in prune policies, zrepl uses Go's `regexp.Regexp <https://golang.org/pkg/regexp/#Compile>`_ Perl-compatible regular expressions (`Syntax <https://golang.org/pkg/regexp/syntax>`_).
The optional `negate` boolean field inverts the semantics: Use it if you want to keep all snapshots that *do not* match the given regex.


