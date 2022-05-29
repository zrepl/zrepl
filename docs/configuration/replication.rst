.. include:: ../global.rst.inc


Replication Options
===================


::
   
   jobs:
   - type: push
     filesystems: ...
     replication:
       protection:
         initial:     guarantee_resumability # guarantee_{resumability,incremental,nothing}
         incremental: guarantee_resumability # guarantee_{resumability,incremental,nothing}
       concurrency:
         size_estimates: 4
         steps: 1

     ...

.. _replication-option-protection:

``protection`` Option
--------------------------

The ``protection`` variable controls the degree to which a replicated filesystem is protected from getting out of sync through a zrepl pruner or external tools that destroy snapshots.
zrepl can guarantee :ref:`resumability <step-holds>` or just :ref:`incremental replication <replication-cursor-and-last-received-hold>`.

``guarantee_resumability`` is the **default** value and guarantees that a replication step is always resumable and that incremental replication will always be possible.
The implementation uses replication cursors, last-received-hold and step holds.

``guarantee_incremental`` only guarantees that incremental replication will always be possible.
If a step ``from -> to`` is interrupted and its `to` snapshot is destroyed, zrepl will remove the half-received ``to``'s resume state and start a new step ``from -> to2``.
The implementation uses replication cursors, tentative replication cursors and last-received-hold.

``guarantee_nothing`` does not make any guarantees with regards to keeping sending and receiving side in sync.
No bookmarks or holds are created to protect sender and receiver from diverging.

**Tradeoffs**

Using ``guarantee_incremental`` instead of ``guarantee_resumability`` obviously removes the resumability guarantee.
This means that replication progress is no longer monotonic which might lead to a replication setup that never makes progress if mid-step interruptions are too frequent (e.g. frequent network outages).
However, the advantage and :issue:`reason for existence <288>` of the ``incremental`` mode is that it allows the pruner to delete snapshots of interrupted replication steps
which is useful if replication happens so rarely (or fails so frequently) that the amount of disk space exclusively referenced by the step's snapshots becomes intolerable.

.. NOTE::

   When changing this flag, obsoleted zrepl-managed bookmarks and holds will be destroyed on the next replication step that is attempted for each filesystem.


.. _replication-option-concurrency:

``concurrency`` Option
----------------------

The ``concurrency`` options control the maximum amount of concurrency during replication.
The default values allow some concurrency during size estimation but no parallelism for the actual replication.

* ``concurrency.steps`` (default = 1) controls the maximum number of concurrently executed :ref:`replication steps <overview-how-replication-works>`.
  The planning step for each file system is counted as a single step.
* ``concurrency.size_estimates`` (default = 4) controls the maximum number of concurrent step size estimations done by the job.

Note that initial replication cannot start replicating child filesystems before the parent filesystem's initial replication step has completed.

Some notes on tuning these values:

* Disk: Size estimation is less I/O intensive than step execution because it does not need to access the data blocks.
* CPU: Size estimation is usually a dense CPU burst whereas step execution CPU utilization is stretched out over time because of disk IO.
  Faster disks, sending a compressed dataset in :ref:`plain mode <zfs-background-knowledge-plain-vs-raw-sends>` and the zrepl transport mode all contribute to higher CPU requirements.
* Network bandwidth: Size estimation does not consume meaningful amounts of bandwidth, step execution does.
* :ref:`zrepl ZFS abstractions <zrepl-zfs-abstractions>`: for each replication step zrepl needs to update its ZFS abstractions through the ``zfs`` command which often waits multiple seconds for the zpool to sync.
  Thus, if the actual send & recv time of a step is small compared to the time spent on zrepl ZFS abstractions then increasing step execution concurrency will result in a lower overall turnaround time.
