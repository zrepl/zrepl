.. _prune:

Pruning Policies
================

In zrepl, *pruning* means *destroying snapshots*.
Pruning must happen on both sides of a replication or the systems would inevitably run out of disk space at some point.

Typically, the requirements to temporal resolution and maximum retention time differ per side.
For example, when using zrepl to back up a busy database server, you will want high temporal resolution (snapshots every 10 min) for the last 24h in case of administrative disasters, but cannot afford to store them for much longer because you might have high turnover volume in the database.
On the receiving side, you may have more disk space available, or need to comply with other backup retention policies.

zrepl uses a set of  **keep rules** per sending and receiving side to determine which snapshots shall be kept per filesystem.
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
           - type: regex
             regex: "^manual_.*"

.. DANGER::
    You might have **existing snapshots** of filesystems affected by pruning which you want to keep, i.e. not be destroyed by zrepl.
    Make sure to actually add the necessary ``regex`` keep rules on both sides, like with ``manual`` in the example above.

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
The state required to evaluate this rule is stored in the :ref:`replication cursor bookmark <replication-cursor-and-last-received-hold>` on the sending side.

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
                │                │               │
                └─ 1 repetition of a one-hour interval with keep=all
                                 │               │
                                 └─ 24 repetitions of a one-hour interval with keep=1
                                                 │
                                                 └─ 6 repetitions of a 30-day interval with keep=1
      ...

The retention grid can be thought of as a time-based sieve that thins out snapshots as they get older.

The ``grid`` field specifies a list of adjacent time intervals.
Each interval is a bucket with a maximum capacity of ``keep`` snapshots.
The following procedure happens during pruning:

#. The list of snapshots is filtered by the regular expression in ``regex``.
   Only snapshots names that match the regex are considered for this rule, all others will be pruned unless another rule keeps them.
#. The snapshots that match ``regex`` are placed onto a time axis according to their ``creation`` date.
   The youngest snapshot is on the left, the oldest on the right.
#. The first buckets are placed "under" that axis so that the ``grid`` spec's first bucket's left edge aligns with youngest snapshot.
#. All subsequent buckets are placed adjacent to their predecessor bucket.
#. Now each snapshot on the axis either falls into one bucket or it is older than our rightmost bucket.
   Buckets are left-inclusive and right-exclusive which means that a snapshot on the edge of bucket will always 'fall into the right one'.
#. Snapshots older than the rightmost bucket **not kept** by this gridspec.
#. For each bucket, we only keep the ``keep`` oldest snapshots.

The syntax to describe the bucket list is as follows:

::

     Repeat x Duration (keep=all)

* The **duration** specifies the length of the interval.
* The **keep** count specifies the number of snapshots that fit into the bucket.
  It can be either a positive integer or ``all`` (all snapshots are kept).
* The **repeat** count repeats the bucket definition for the specified number of times.

**Example**:

::

          This grid spec produces the following list of adjacent buckets. For the sake of simplicity,
          we subject all snapshots to the grid pruning policy by settings `regex: .*`.

          `
           grid: 1x1h(keep=all) | 2x2h | 1x3h
           regex: .*
          `

          0h        1h        2h        3h        4h        5h        6h        7h        8h        9h
          |         |         |         |         |         |         |         |         |         |
          |-Bucket1-|-----Bucket 2------|------Bucket 3-----|-----------Bucket 4----------|
          | keep=all|      keep=1       |       keep=1      |            keep=1           |



          Let us consider the following set of snapshots @a-zA-C:


          |  a  b  c  d  e  f  g  h  i  j  k  l  m  n  o  p  q  r  s  t  u  v  w  x  y  z  A  B  C  D        |

          The `grid` algorithm maps them to their respective buckets:

          Bucket 1: a, b, c
          Bucket 2: d,e,f,g,h,i,j
          Bucket 3: k,l,m,n,o,p
          Bucket 4: q,r, q,r,s,t,u,v,w,x,y,z
          None:     A,B,C,D

          It then applies the per-bucket pruning logic described above which resulting in the
          following list of remaining snapshots.

          |  a  b  c                    j                 p                             z                    |

          Note that it only makes sense to grow (not shorten) the interval duration for buckets
          further in the past since each bucket acts like a low-pass filter for incoming snapshots
          and adding a less-low-pass-filter after a low-pass one has no effect.


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
           regex: ^zrepl_.*$ # optional
     ...

``last_n`` filters the snapshot list by ``regex``, then keeps the last ``count`` snapshots in that list (last = youngest = most recent creation date)
All snapshots that don't match ``regex`` or exceed ``count`` in the filtered list are destroyed unless matched by other rules.

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

``regex`` keeps all snapshots whose names are matched by the regular expression in ``regex``.
Like all other regular expression fields in prune policies, zrepl uses Go's `regexp.Regexp <https://golang.org/pkg/regexp/#Compile>`_ Perl-compatible regular expressions (`Syntax <https://golang.org/pkg/regexp/syntax>`_).
The optional `negate` boolean field inverts the semantics: Use it if you want to keep all snapshots that *do not* match the given regex.

.. _prune-workaround-source-side-pruning:

Source-side snapshot pruning
----------------------------

A :ref:`source jobs<job-source>` takes snapshots on the system it runs on.
The corresponding :ref:`pull job <job-pull>` on the replication target connects to the source job and replicates the snapshots.
Afterwards, the pull job coordinates pruning on both sender (the source job side) and receiver (the pull job side).

There is no built-in way to define and execute pruning on the source side independently of the pull side.
The source job will continue taking snapshots which will not be pruned until the pull side connects.
This means that **extended replication downtime will fill up the source's zpool with snapshots**.

If the above is a conceivable situation for you, consider using :ref:`push mode <job-push>`, where pruning happens on the same side where snapshots are taken.

Workaround using ``snap`` job
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

As a workaround (see GitHub :issue:`102` for development progress), a pruning-only :ref:`snap job <job-snap>` can be defined on the source side:
The snap job is in charge of snapshot creation & destruction, whereas the source job's role is reduced to just serving snapshots.
However, since, jobs are run independently, it is possible that the snap job will prune snapshots that are queued for replication / destruction by the remote pull job that connects to the source job.
Symptoms of such race conditions are spurious replication and destroy errors.

Example configuration:

::

  # source side
  jobs:
  - type: snap
    snapshotting:
      type: periodic
    pruning:
      keep:
        # source side pruning rules go here
    ...

  - type: source
    snapshotting:
      type: manual
    root_fs: ...

  # pull side
  jobs:
  - type: pull
    pruning:
      keep_sender:
        # let the source-side snap job do the pruning
        - type: regex
          regex: ".*"
        ...
      keep_receiver:
        # feel free to prune on the pull side as desired
        ...
