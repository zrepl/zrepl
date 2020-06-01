
Overview & Terminology
======================

All work zrepl does is performed by the zrepl daemon which is configured in a single YAML configuration file loaded on startup.
The following paths are considered:

* If set, the location specified via the global ``--config`` flag
* ``/etc/zrepl/zrepl.yml``
* ``/usr/local/etc/zrepl/zrepl.yml``

The ``zrepl configcheck`` subcommand can be used to validate the configuration.
The command will output nothing and exit with zero status code if the configuration is valid.
The error messages vary in quality and usefulness: please report confusing config errors to the tracking :issue:`155`.
Full example configs such as in the :ref:`tutorial` or the :sampleconf:`/` directory might also be helpful.
However, copy-pasting examples is no substitute for reading documentation!

Config File Structure
---------------------

.. code-block:: yaml

   global: ...
   jobs:
   - name: backup
     type: push
   - ...

zrepl is configured using a single YAML configuration file with two main sections: ``global`` and ``jobs``.
The ``global`` section is filled with sensible defaults and is covered later in this chapter.
The ``jobs`` section is a list of jobs which we are going to explain now.

.. _job-overview:

Jobs \& How They Work Together
------------------------------

A *job* is the unit of activity tracked by the zrepl daemon.
The ``type`` of a job determines its role in a replication setup and in snapshot management.
Jobs are identified by their ``name``, both in log files and the ``zrepl status`` command.

Replication always happens between a pair of jobs: one is the **active side**, and one the **passive side**.
The active side connects to the passive side using a :ref:`transport <transport>` and starts executing the replication logic.
The passive side responds to requests from the active side after checking its permissions.

The following table shows how different job types can be combined to achieve **both push and pull mode setups**.
Note that snapshot-creation denoted by "(snap)" is orthogonal to whether a job is active or passive.

+-----------------------+--------------+----------------------------------+------------------------------------------------------------------------------------+
| Setup name            | active side  | passive side                     | use case                                                                           |
+=======================+==============+==================================+====================================================================================+
| Push mode             | ``push``     | ``sink``                         | * Laptop backup                                                                    |
|                       | (snap)       |                                  | * NAS behind NAT to offsite                                                        |
+-----------------------+--------------+----------------------------------+------------------------------------------------------------------------------------+
| Pull mode             | ``pull``     | ``source``                       | * Central backup-server for many nodes                                             |
|                       |              | (snap)                           | * Remote server to NAS behind NAT                                                  |
+-----------------------+--------------+----------------------------------+------------------------------------------------------------------------------------+
| Local replication     | | ``push`` + ``sink`` in one config             | * Backup FreeBSD boot pool                                                         |
|                       | | with :ref:`local transport <transport-local>` |                                                                                    |
+-----------------------+--------------+----------------------------------+------------------------------------------------------------------------------------+
| Snap & prune-only     | ``snap``     | N/A                              | * | Snapshots & pruning but no replication                                         |
|                       | (snap)       |                                  |   | required                                                                       |
|                       |              |                                  | * Workaround for :ref:`source-side pruning <prune-workaround-source-side-pruning>` |
+-----------------------+--------------+----------------------------------+------------------------------------------------------------------------------------+

How the Active Side Works
-------------------------

The active side (:ref:`push <job-push>` and :ref:`pull <job-pull>` job) executes the replication and pruning logic:

* Wakeup because of finished snapshotting (``push`` job) or pull interval ticker (``pull`` job).
* Connect to the corresponding passive side using a :ref:`transport <transport>` and instantiate an RPC client.
* Replicate data from the sending to the receiving side (see below).
* Prune on sender & receiver.

.. TIP::
  The progress of the active side can be watched live using the ``zrepl status`` subcommand.

How the Passive Side Works
--------------------------

The passive side (:ref:`sink <job-sink>` and :ref:`source <job-source>`) waits for connections from the corresponding active side,
using the transport listener type specified in the ``serve`` field of the job configuration.
Each transport listener provides a client's identity to the passive side job.
It uses the client identity for access control:

* The ``sink`` job maps requests from different client identities to their respective sub-filesystem tree ``root_fs/${client_identity}``.
* The ``source`` job has a whitelist of client identities that are allowed pull access.

.. TIP::
   The implementation of the ``sink`` job requires that the connecting client identities be a valid ZFS filesystem name components.

.. _overview-how-replication-works:

How Replication Works
---------------------

One of the major design goals of the replication module is to avoid any duplication of the nontrivial logic.
As such, the code works on abstract senders and receiver **endpoints**, where typically one will be implemented by a local program object and the other is an RPC client instance.
Regardless of push- or pull-style setup, the logic executes on the active side, i.e. in the ``push`` or ``pull`` job.

The following steps take place during replication and can be monitored using the ``zrepl status`` subcommand:

* Plan the replication:

  * Compare sender and receiver filesystem snapshots
  * Build the **replication plan**

    * Per filesystem, compute a diff between sender and receiver snapshots
    * Build a list of replication steps

      * If possible, use incremental and resumable sends (``zfs send -i``)
      * Otherwise, use full send of most recent snapshot on sender

  * Retry on errors that are likely temporary (i.e. network failures).
  * Give up on filesystems where a permanent error was received over RPC.

* Execute the plan

  * Perform replication steps in the following order:
    Among all filesystems with pending replication steps, pick the filesystem whose next replication step's snapshot is the oldest.
  * Create placeholder filesystems on the receiving side to mirror the dataset paths on the sender to ``root_fs/${client_identity}``.
  * Acquire send-side *step-holds* on the step's `from` and `to` snapshots.
  * Perform the replication step.
  * Move the **replication cursor** bookmark on the sending side (see below).
  * Move the **last-received-hold** on the receiving side (see below).
  * Release the send-side step-holds.
   
The idea behind the execution order of replication steps is that if the sender snapshots all filesystems simultaneously at fixed intervals, the receiver will have all filesystems snapshotted at time ``T1`` before the first snapshot at ``T2 = T1 + $interval`` is replicated.

.. _replication-cursor-and-last-received-hold:

**Replication cursor** bookmark and **last-received-hold** are managed by zrepl to ensure that future replications can always be done incrementally:
the replication cursor is a send-side bookmark of the most recent successfully replicated snapshot,
and the last-received-hold is a hold of that snapshot on the receiving side.
The replication cursor has the format ``#zrepl_CUSOR_G_<GUID>_J_<JOBNAME>``.
The last-received-hold tag has the format ``#zrepl_last_received_J_<JOBNAME>``.
Encoding the job name in the names ensures that multiple sending jobs can replicate the same filesystem to different receivers without interference.
The ``zrepl holds list`` provides a listing of all bookmarks and holds managed by zrepl.

.. _replication-placeholder-property:

**Placeholder filesystems** on the receiving side are regular ZFS filesystems with the placeholder property ``zrepl:placeholder=on``.
Placeholders allow the receiving side to mirror the sender's ZFS dataset hierarchy without replicating every filesystem at every intermediary dataset path component.
Consider the following example: ``S/H/J`` shall be replicated to ``R/sink/job/S/H/J``, but neither ``S/H`` nor ``S`` shall be replicated.
ZFS requires the existence of ``R/sink/job/S`` and ``R/sink/job/S/H`` in order to receive into ``R/sink/job/S/H/J``.
Thus, zrepl creates the parent filesystems as placeholders on the receiving side.
If at some point ``S/H`` and ``S`` shall be replicated, the receiving side invalidates the placeholder flag automatically.
The ``zrepl test placeholder`` command can be used to check whether a filesystem is a placeholder.

.. ATTENTION::

    Currently, zrepl does not replicate filesystem properties.
    When receiving a filesystem, it is never mounted (`-u` flag)  and `mountpoint=none` is set.
    This is temporary and being worked on :issue:`24`.

.. NOTE::

    More details can be found in the design document :repomasterlink:`replication/design.md`.


.. _jobs-multiple-jobs:

Multiple Jobs & More than 2 Machines
------------------------------------

.. ATTENTION::

  When using multiple jobs across single or multiple machines, the following rules are critical to avoid race conditions & data loss:

  1. The sets of ZFS filesystems matched by the ``filesystems`` filter fields must be disjoint across all jobs configured on a machine.
  2. The ZFS filesystem subtrees of jobs with ``root_fs`` must be disjoint.
  3. Across all zrepl instances on all machines in the replication domain, there must be a 1:1 correspondence between active and passive jobs.

  Explanations & exceptions to above rules are detailed below.

If you would like to see improvements to multi-job setups, please `open an issue on GitHub <https://github.com/zrepl/zrepl/issues/new>`_.

No Overlapping
^^^^^^^^^^^^^^

Jobs run independently of each other.
If two jobs match the same filesystem with their ``filesystems`` filter, they will operate on that filesystem independently and potentially in parallel.
For example, if job A prunes snapshots that job B is planning to replicate, the replication will fail because B assumed the snapshot to still be present.
However, the next replication attempt will re-examine the situation from scratch and should work.

N push jobs to 1 sink
^^^^^^^^^^^^^^^^^^^^^

The :ref:`sink job <job-sink>` namespaces by client identity.
It is thus safe to push to one sink job with different client identities.
If the push jobs have the same client identity, the filesystems matched by the push jobs must be disjoint to avoid races.

N pull jobs from 1 source
^^^^^^^^^^^^^^^^^^^^^^^^^

Multiple pull jobs pulling from the same source have potential for race conditions during pruning:
each pull job prunes the source side independently, causing replication-prune and prune-prune races.

There is currently no way for a pull job to filter which snapshots it should attempt to replicate.
Thus, it is not possible to just manually assert that the prune rules of all pull jobs are disjoint to avoid replication-prune and prune-prune races.

