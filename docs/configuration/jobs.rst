.. include:: ../global.rst.inc

.. |patient| replace:: :ref:`patient <job-term-patient>`
.. |serve-transport| replace:: :ref:`serve transport<transport-ssh+stdinserver-serve>`
.. |connect-transport| replace:: :ref:`connect transport<transport-ssh+stdinserver-connect>`
.. |mapping| replace:: :ref:`mapping <pattern-mapping>`
.. |filter| replace:: :ref:`filter <pattern-filter>`
.. |prune| replace:: :ref:`prune <prune>`

.. _job:

Job Types
=========

A *job* is the unit of activity tracked by the zrepl daemon and configured in the |mainconfig|.
Every job has a unique ``name``, a ``type`` and type-dependent fields which are documented on this page.
Check out the :ref:`tutorial` and :sampleconf:`/` for examples on how job types are actually used.

.. ATTENTION::

    Currently, zrepl does not replicate filesystem properties.
    Whe receiving a filesystem, it is never mounted (`-u` flag)  and `mountpoint=none` is set.
    This is temporary and being worked on :issue:`24`.

.. _job-source:

Source Job
----------

Example: :sampleconf:`pullbackup/productionhost.yml`.

.. list-table::
    :widths: 20 80
    :header-rows: 1

    * - Parameter
      - Comment
    * - ``type``
      - = ``source``
    * - ``name``
      - unique name of the job
    * - ``serve``
      - |serve-transport|
    * - ``filesystems``
      - |filter| for filesystems to expose to client
    * - ``snapshot_prefix``
      - prefix for ZFS snapshots taken by this job
    * - ``interval``
      - snapshotting interval
    * - ``prune``
      - |prune| policy for filesytems in ``filesystems`` with prefix ``snapshot_prefix``


- Snapshotting Task (every ``interval``, |patient|)

  - A snapshot of filesystems matched by ``filesystems`` is taken every ``interval`` with prefix ``snapshot_prefix``.
  - A bookmark of that snapshot is created with the same name.
  - The ``prune`` policy is triggered on filesystems matched by ``filesystems`` with snapshots matched by ``snapshot_prefix``.

- Serve Task

  - Wait for connections from pull job using ``serve``.

A source job is the counterpart to a :ref:`job-pull`.

Make sure you read the |prune| policy documentation.

Note that zrepl does not prune bookmarks due to the following reason:
a pull job may stop replication due to link failure, misconfiguration or administrative action.
The source prune policy will eventually destroy the last common snapshot between source and pull job.
Without bookmarks, the prune policy would need to perform full replication again.
With bookmarks, we can resume incremental replication, only losing the snapshots pruned since the outage.

.. _job-pull:

Pull Job
--------

Example: :sampleconf:`pullbackup/backuphost.yml`

.. list-table::
    :widths: 20 80
    :header-rows: 1

    * - Parameter
      - Comment
    * - ``type``
      - = ``pull``
    * - ``name``
      - unqiue name of the job
    * - ``connect``
      - |connect-transport|
    * - ``interval``
      - Interval between pull attempts
    * - ``mapping``
      - |mapping| for remote to local filesystems
    * - ``initial_repl_policy``
      - default = ``most_recent``, initial replication policy
    * - ``snapshot_prefix``
      - prefix snapshots must match to be considered for replication & pruning
    * - ``prune``
      - |prune| policy for local filesystems reachable by ``mapping``

* Main Task (every ``interval``, |patient|)
 
  #. A connection to the remote source job is established using the strategy in ``connect``
  #. ``mapping`` maps filesystems presented by the remote side to local *target filesystems*
  #. Those remote filesystems with a local *target filesystem* are replicated
 
     #. Only snapshots with prefix ``snapshot_prefix`` are replicated.
     #. If possible, incremental replication takes place.
     #. If the local target filesystem does not exist, ``initial_repl_policy`` is used.
     #. On conflicts, an error is logged but replication of other filesystems with mapping continues.
  
  #. The ``prune`` policy is triggered for all *target filesystems*

A pull job is the counterpart to a :ref:`job-source`.


.. _job-local:

Local Job
---------

Example: :sampleconf:`localbackup/host1.yml`

.. list-table::
    :widths: 20 80
    :header-rows: 1

    * - Parameter
      - Comment
    * - ``type``
      - = ``local``
    * - ``name``
      - unqiue name of the job
    * - ``mapping``
      - |mapping| from source to target filesystem (both local)
    * - ``snapshot_prefix``
      - prefix for ZFS snapshots taken by this job
    * - ``interval``
      - snapshotting & replication interval
    * - ``initial_repl_policy``
      - default = ``most_recent``, initial replication policy
    * - ``prune_lhs``
      -  pruning policy on left-hand-side (source)
    * - ``prune_rhs``
      - pruning policy on right-hand-side (target)

* Main Task (every ``interval``, |patient|)

  #. Evaluate ``mapping`` for local filesystems, those with a *target filesystem* are called *mapped filesystems*.
  #. Snapshot *mapped filesystems* with ``snapshot_prefix``.
  #. Bookmark the snapshot created above.
  #. Replicate *mapped filesystems* to their respective *target filesystems*:

     #. Only snapshots with prefix ``snapshot_prefix`` are replicated.
     #. If possible, incremental replication takes place.
     #. If the *target filesystem* does not exist, ``initial_repl_policy`` is used.
     #. On conflicts, an error is logged but replication of other *mapped filesystems* continues.

  #. The ``prune_lhs`` policy is triggered for all *mapped filesystems*
  #. The ``prune_rhs`` policy is triggered for all *target filesystems*

A local job is combination of source & pull job executed on the same machine.
Note that while snapshots are pruned, bookmarks are not pruned and kept around forever.
Refer to the comments on :ref:`source job <job-source>` for the reasoning behind this.

Terminology
-----------

task

    A job consists of one or more tasks and a task consists of one or more steps.
    Some tasks may be periodic while others wait for an event to occur.


patient task

    .. _job-term-patient:

    A patient task is supposed to execute some task every `interval`.
    We call the start of the task an *invocation*.

    * If the task completes in less than `interval`, the task is restarted at `last_invocation + interval`.
    * Otherwise, a patient job
        * logs a warning as soon as a task exceeds its configured `interval`
        * waits for the last invocation to finish
        * logs a warning with the effective task duration
        * immediately starts a new invocation of the task
