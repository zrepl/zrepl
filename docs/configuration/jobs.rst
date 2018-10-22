.. include:: ../global.rst.inc

.. |serve-transport| replace:: :ref:`serve specification<transport>`
.. |connect-transport| replace:: :ref:`connect specification<transport>`
.. |snapshotting-spec| replace:: :ref:`snapshotting specification <job-snapshotting-spec>`
.. |pruning-spec| replace:: :ref:`pruning specification <prune>`
.. |filter-spec| replace:: :ref:`filter specification<pattern-filter>`

.. _job:

Job Types & Replication
=======================

Overview & Terminology
----------------------

A *job* is the unit of activity tracked by the zrepl daemon and configured in the |mainconfig|.
Every job has a unique ``name``, a ``type`` and type-dependent fields which are documented on this page.

Replication always happens between a pair of jobs: one is the **active side**, and one the **passive side**.
The active side executes the replication logic whereas the passive side responds to requests after checking the active side's permissions.
For communication, the active side connects to the passive side using a :ref:`transport <transport>` and starts issuing remote procedure calls (RPCs).

The following table shows how different job types can be combined to achieve both push and pull mode setups:

+-----------------------+--------------+----------------------------------+-----------------------------------------------+
| Setup name            | active side  | passive side                     | use case                                      |
+=======================+==============+==================================+===============================================+
| Push mode             | ``push``     | ``sink``                         | * Laptop backup                               |
|                       |              |                                  | * NAS behind NAT to offsite                   |
+-----------------------+--------------+----------------------------------+-----------------------------------------------+
| Pull mode             | ``pull``     | ``source``                       | * Central backup-server for many nodes        |
|                       |              |                                  | * Remote server to NAS behind NAT             |
+-----------------------+--------------+----------------------------------+-----------------------------------------------+
| Local replication     | | ``push`` + ``sink`` in one config             | * Backup FreeBSD boot pool                    |
|                       | | with :ref:`local transport <transport-local>` |                                               |
+-----------------------+--------------+----------------------------------+-----------------------------------------------+

How the Active Side Works
~~~~~~~~~~~~~~~~~~~~~~~~~

The active side (:ref:`push <job-push>` and :ref:`pull <job-pull>` job) executes the replication and pruning logic:

* Wakeup because of finished snapshotting (``push`` job) or pull interval ticker (``pull`` job).
* Connect to the corresponding passive side using a :ref:`transport <transport>` and instantiate an RPC client.
* Replicate data from the sending to the receiving side.
* Prune on sender & receiver.

.. TIP::
  The progress of the active side can be watched live using the ``zrepl status`` subcommand.

How the Passive Side Works
~~~~~~~~~~~~~~~~~~~~~~~~~~

The passive side (:ref:`sink <job-sink>` and :ref:`source <job-source>`) waits for connections from the corresponding active side,
using the transport listener type specified in the ``serve`` field of the job configuration.
Each transport listener provides a client's identity to the passive side job.
It uses the client identity for access control:

* The ``sink`` job only allows pushes to those ZFS filesystems to the active side that are located below ``root_fs/${client_identity}``.
* The ``source`` job has a whitelist of client identities that are allowed pull access.

.. TIP::
   The implementation of the ``sink`` job requires that the connecting client identities be a valid ZFS filesystem name components.

How Replication Works
~~~~~~~~~~~~~~~~~~~~~

One of the major design goals of the replication module is to avoid any duplication of the nontrivial logic.
As such, the code works on abstract senders and receiver **endpoints**, where typically one will be implemented by a local program object and the other is an RPC client instance.
Regardless of push- or pull-style setup, the logic executes on the active side, i.e. in the ``push`` or ``pull`` job.

The following steps take place during replication and can be monitored using the ``zrepl status`` subcommand:

* Plan the replication:

  * Compare sender and receiver filesystem snapshots
  * Build the **replication plan**

    * Per filesystem, compute a diff between sender and receiver snapshots
    * Build a list of replication steps

      * If possible, use incremental sends (``zfs send -i``)
      * Otherwise, use full send of most recent snapshot on sender
      * Give up on filesystems that cannot be replicated without data loss

  * Retry on errors that are likely temporary (i.e. network failures).
  * Give up on filesystems where a permanent error was received over RPC.

* Execute the plan

  * Perform replication steps in the following order:
    Among all filesystems with pending replication steps, pick the filesystem whose next replication step's snapshot is the oldest.
  * After a successful replication step, update the replication cursor bookmark (see below)
   
The idea behind the execution order of replication steps is that if the sender snapshots all filesystems simultaneously at fixed intervals, the receiver will have all filesystems snapshotted at time ``T1`` before the first snapshot at ``T2 = T1 + $interval`` is replicated.

.. _replication-cursor-bookmark:

The **replication cursor bookmark** ``#zrepl_replication_cursor`` is kept per filesystem on the sending side of a replication setup:
It is a bookmark of the most recent successfully replicated snapshot to the receiving side.
It is is used by the :ref:`not_replicated <prune-keep-not-replicated>` keep rule to identify all snapshots that have not yet been replicated to the receiving side.
Regardless of whether that keep rule is used, the bookmark ensures that replication can always continue incrementally.

.. ATTENTION::

    Currently, zrepl does not replicate filesystem properties.
    Whe receiving a filesystem, it is never mounted (`-u` flag)  and `mountpoint=none` is set.
    This is temporary and being worked on :issue:`24`.


.. _job-snapshotting-spec:

Taking Snaphots
---------------

The ``push`` and ``source`` jobs can automatically take periodic snapshots of the filesystems matched by the ``filesystems`` filter field.
The snapshot names are composed of a user-defined prefix followed by a UTC date formatted like ``20060102_150405_000``.
We use UTC because it will avoid name conflicts when switching time zones or between summer and winter time.

For ``push`` jobs, replication is automatically triggered after all filesystems have been snapshotted.

::

    jobs:
    - type: push
      filesystems: {
        "<": true,
        "tmp": false
      }
      snapshotting:
        type: periodic
        prefix: zrepl_
        interval: 10m
      ...

There is also a ``manual`` snapshotting type, which covers the following use cases:

* Existing infrastructure for automatic snapshots: you only want to use zrepl for replication.
* Run scripts before and after taking snapshots (like locking database tables).
  We are working on better integration for this use case: see :issue:`74`.

Note that you will have to trigger replication manually using the ``zrepl wakeup JOB`` subcommand in that case.

::

   jobs:
   - type: push
     filesystems: {
       "<": true,
       "tmp": false
     }
     snapshotting:
       type: manual
     ...


.. _job-push:

Job Type ``push``
-----------------

.. list-table::
    :widths: 20 80
    :header-rows: 1

    * - Parameter
      - Comment
    * - ``type``
      - = ``push``
    * - ``name``
      - unique name of the job
    * - ``connect``
      - |connect-transport|
    * - ``filesystems``
      - |filter-spec| for filesystems to be snapshotted and pushed to the sink
    * - ``snapshotting``
      - |snapshotting-spec|
    * - ``pruning``
      - |pruning-spec|

Example config: :sampleconf:`/push.yml`

.. _job-sink:

Job Type ``sink``
-----------------

.. list-table::
    :widths: 20 80
    :header-rows: 1

    * - Parameter
      - Comment
    * - ``type``
      - = ``sink``
    * - ``name``
      - unique name of the job
    * - ``serve``
      - |serve-transport|
    * - ``root_fs``
      - ZFS dataset path are received to
        ``$root_fs/$client_identity``

Example config: :sampleconf:`/sink.yml`

.. _job-pull:

Job Type ``pull``
-----------------

.. list-table::
    :widths: 20 80
    :header-rows: 1

    * - Parameter
      - Comment
    * - ``type``
      - = ``pull``
    * - ``name``
      - unique name of the job
    * - ``connect``
      - |connect-transport|
    * - ``root_fs``
      - ZFS dataset path are received to
        ``$root_fs/$client_identity``
    * - ``interval``
      - Interval at which to pull from the source job
    * - ``pruning``
      - |pruning-spec|

Example config: :sampleconf:`/pull.yml`

.. _job-source:

Job Type ``source``
-------------------

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
      - |filter-spec| for filesystems to be snapshotted and exposed to connecting clients
    * - ``snapshotting``
      - |snapshotting-spec|

Example config: :sampleconf:`/source.yml`

.. _replication-local:

Local replication
-----------------

If you have the need for local replication (most likely between two local storage pools), you can use the :ref:`local transport type <transport-local>` to connect a local push job to a local sink job.

Example config: :sampleconf:`/local.yml`.


