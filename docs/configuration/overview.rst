
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
Full example configs such as in the :ref:`quick-start guides <quickstart-toc>` or the :sampleconf:`/` directory might also be helpful.
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

.. NOTE::
   The job name is persisted in several places on disk and thus :issue:`cannot be changed easily<327>`.


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
| Local replication     | | ``push`` + ``sink`` in one config             | * Backup to :ref:`locally attached disk <quickstart-backup-to-external-disk>`      |
|                       | | with :ref:`local transport <transport-local>` | * Backup FreeBSD boot pool                                                         |
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

.. _overview-passive-side--client-identity:

How the Passive Side Works
--------------------------

The passive side (:ref:`sink <job-sink>` and :ref:`source <job-source>`) waits for connections from the corresponding active side,
using the transport listener type specified in the ``serve`` field of the job configuration.
When a client connects, the transport listener performS listener-specific access control (cert validation, IP ACLs, etc)
and determines the *client identity*.
The passive side job then uses this client identity as follows:

* The ``sink`` job maps requests from different client identities to their respective sub-filesystem tree ``root_fs/${client_identity}``.
* The ``source`` might, in the future, embed the client identity in :ref:`zrepl's ZFS abstraction names <zrepl-zfs-abstractions>` in order to support multi-host replication.

.. TIP::
   The implementation of the ``sink`` job requires that the connecting client identities be a valid ZFS filesystem name components.

.. _overview-how-replication-works:

How Replication Works
---------------------

One of the major design goals of the replication module is to avoid any duplication of the nontrivial logic.
As such, the code works on abstract senders and receiver **endpoints**, where typically one will be implemented by a local program object and the other is an RPC client instance.
Regardless of push- or pull-style setup, the logic executes on the active side, i.e. in the ``push`` or ``pull`` job.

The following high-level steps take place during replication and can be monitored using the ``zrepl status`` subcommand:

* Plan the replication:

  * Compare sender and receiver filesystem snapshots
  * Build the **replication plan**

    * Per filesystem, compute a diff between sender and receiver snapshots
    * Build a list of **replication steps**

      * If possible, use incremental and resumable sends
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

ZFS Background Knowledge
^^^^^^^^^^^^^^^^^^^^^^^^
This section gives some background knowledge about ZFS features that zrepl uses to provide guarantees for a replication filesystem.
Specifically, zrepl guarantees by default that **incremental replication is always possible and that started replication steps can always be resumed if they are interrupted.**

**ZFS Send Modes & Bookmarks**
ZFS supports full sends (``zfs send fs@to``) and incremental sends (``zfs send -i @from fs@to``).
Full sends are used to create a new filesystem on the receiver with the send-side state of ``fs@to``.
Incremental sends only transfer the delta between ``@from`` and ``@to``.
Incremental sends require that ``@from`` be present on the receiving side when receiving the incremental stream.
Incremental sends can also use a ZFS bookmark as *from* on the sending side (``zfs send -i #bm_from fs@to``), where ``#bm_from`` was created using ``zfs bookmark fs@from fs#bm_from``.
The receiving side must always have the actual snapshot ``@from``, regardless of whether the sending side uses ``@from`` or a bookmark of it.

.. _zfs-background-knowledge-plain-vs-raw-sends:

**Plain and raw sends**
By default, ``zfs send`` sends the most generic, backwards-compatible data stream format (so-called 'plain send').
If the sent uses newer features, e.g. compression or encryption, ``zfs send`` has to un-do these operations on the fly to produce the plain send stream.
If the receiver uses newer features (e.g. compression or encryption inherited from the parent FS), it applies the necessary transformations again on the fly during ``zfs recv``.

Flags such as ``-e``, ``-c`` and ``-L``  tell ZFS to produce a send stream that is closer to how the data is stored on disk.
Sending with those flags removes computational overhead from sender and receiver.
However, the receiver will not apply certain transformations, e.g., it will not compress with the receive-side ``compression`` algorithm.

The ``-w`` (``--raw``) flag produces a send stream that is as *raw* as possible.
For unencrypted datasets, its current effect is the same as ``-Lce``.

Encrypted datasets can only be sent plain (unencrypted) or raw (encrypted) using the ``-w`` flag.

**Resumable Send & Recv**
The ``-s`` flag for ``zfs recv`` tells zfs to save the partially received send stream in case it is interrupted.
To resume the replication, the receiving side filesystem's ``receive_resume_token`` must be passed to a new ``zfs send -t <value> | zfs recv`` command.
A full send can only be resumed if ``@to`` still exists.
An incremental send can only be resumed if ``@to`` still exists *and* either ``@from`` still exists *or* a bookmark ``#fbm`` of ``@from`` still exists.

**ZFS Holds**
ZFS holds prevent a snapshot from being deleted through ``zfs destroy``, letting the destroy fail with a ``datset is busy`` error.
Holds are created and referred to by a *tag*. They can be thought of as a named, persistent lock on the snapshot.


.. _zrepl-zfs-abstractions:

ZFS Abstractions Managed By zrepl
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
With the background knowledge from the previous paragraph, we now summarize the different on-disk ZFS objects that zrepl manages to provide its functionality.

.. _replication-placeholder-property:

**Placeholder filesystems** on the receiving side are regular ZFS filesystems with the ZFS property ``zrepl:placeholder=on``.
Placeholders allow the receiving side to mirror the sender's ZFS dataset hierarchy without replicating every filesystem at every intermediary dataset path component.
Consider the following example: ``S/H/J`` shall be replicated to ``R/sink/job/S/H/J``, but neither ``S/H`` nor ``S`` shall be replicated.
ZFS requires the existence of ``R/sink/job/S`` and ``R/sink/job/S/H`` in order to receive into ``R/sink/job/S/H/J``.
Thus, zrepl creates the parent filesystems as placeholders on the receiving side.
If at some point ``S/H`` and ``S`` shall be replicated, the receiving side invalidates the placeholder flag automatically.
The ``zrepl test placeholder`` command can be used to check whether a filesystem is a placeholder.

.. _replication-cursor-and-last-received-hold:

The **replication cursor** bookmark and **last-received-hold** are managed by zrepl to ensure that future replications can always be done incrementally.
The replication cursor is a send-side bookmark of the most recent successfully replicated snapshot,
and the last-received-hold is a hold of that snapshot on the receiving side.
Both are moved atomically after the receiving side has confirmed that a replication step is complete.

The replication cursor has the format ``#zrepl_CUSOR_G_<GUID>_J_<JOBNAME>``.
The last-received-hold tag has the format ``zrepl_last_received_J_<JOBNAME>``.
Encoding the job name in the names ensures that multiple sending jobs can replicate the same filesystem to different receivers without interference.

.. _tentative-replication-cursor-bookmarks:

**Tentative replication cursor bookmarks** are short-lived bookmarks that protect the atomic moving-forward of the replication cursor and last-received-hold (see :issue:`this issue <340>`).
They are only necessary if step holds are not used as per the :ref:`replication.protection <replication-option-protection>` setting.
The tentative replication cursor has the format ``#zrepl_CUSORTENTATIVE_G_<GUID>_J_<JOBNAME>``.
The ``zrepl zfs-abstraction list`` command provides a listing of all bookmarks and holds managed by zrepl.

.. _step-holds:

**Step holds** are zfs holds managed by zrepl to ensure that a replication step can always be resumed if it is interrupted, e.g., due to network outage.
zrepl creates step holds before it attempts a replication step and releases them after the receiver confirms that the replication step is complete.
For an initial replication ``full @initial_snap``, zrepl puts a zfs hold on ``@initial_snap``.
For an incremental send ``@from -> @to``, zrepl puts a zfs hold on both ``@from`` and ``@to``.
Note that ``@from`` is not strictly necessary for resumability -- a bookmark on the sending side would be sufficient --, but size-estimation in currently used OpenZFS versions only works if ``@from`` is a snapshot.
The hold tag has the format ``zrepl_STEP_J_<JOBNAME>``.
A job only ever has one active send per filesystem.
Thus, there are never more than two step holds for a given pair of ``(job,filesystem)``.

**Step bookmarks** are zrepl's equivalent for holds on bookmarks (ZFS does not support putting holds on bookmarks).
They are intended for a situation where a replication step uses a bookmark ``#bm`` as incremental ``from`` where ``#bm`` is not managed by zrepl.
To ensure resumability, zrepl copies ``#bm`` to step bookmark ``#zrepl_STEP_G_<GUID>_J_<JOBNAME>``.
If the replication is interrupted and ``#bm`` is deleted by the user, the step bookmark remains as an incremental source for the resumable send.
Note that zrepl does not yet support creating step bookmarks because the `corresponding ZFS feature for copying bookmarks <https://github.com/openzfs/zfs/pull/9571>`_ is not yet widely available .
Subscribe to zrepl :issue:`326` for details.

The ``zrepl zfs-abstraction list`` command provides a listing of all bookmarks and holds managed by zrepl.

.. NOTE::

    More details can be found in the design document :repomasterlink:`replication/design.md`.


Caveats With Complex Setups (More Than 2 Jobs or Machines)
----------------------------------------------------------

Most users are served well with a single sender and a single receiver job.
This section documents considerations for more complex setups.

.. ATTENTION::

   Before you continue, make sure you have a working understanding of :ref:`how zrepl works <overview-how-replication-works>`
   and :ref:`what zrepl does to ensure <zrepl-zfs-abstractions>` that replication between sender and receiver is always
   possible without conflicts.
   This will help you understand why certain kinds of multi-machine setups do not (yet) work.

.. NOTE::

   If you can't find your desired configuration, have questions or would like to see improvements to multi-job setups, please `open an issue on GitHub <https://github.com/zrepl/zrepl/issues/new>`_.

Multiple Jobs on One Machine
^^^^^^^^^^^^^^^^^^^^^^^^^^^^
As a general rule, multiple jobs configured on one machine **must operate on disjoint sets of filesystems**.
Otherwise, concurrently running jobs might interfere when operating on the same filesystem.

On your setup, ensure that

* all ``filesystems`` filter specifications are disjoint
* no ``root_fs`` is a prefix or equal to another ``root_fs``
* no ``filesystems`` filter matches any ``root_fs``

**Exceptions to the rule**:

* A ``snap`` and ``push`` job on the same machine can match the same ``filesystems``.
  To avoid interference, only one of the jobs should be pruning snapshots on the sender, the other one should keep all snapshots.
  Since the jobs won't coordinate, errors in the log are to be expected, but :ref:`zrepl's ZFS abstractions <zrepl-zfs-abstractions>` ensure that ``push`` and ``sink`` can always replicate incrementally.
  This scenario is detailed in one of the :ref:`quick-start guides <quickstart-backup-to-external-disk>`.


Two Or More Machines
^^^^^^^^^^^^^^^^^^^^

This section might be relevant to users who wish to *fan-in* (N machines replicate to 1) or *fan-out* (replicate 1 machine to N machines).

**Working setups**:

* **Fan-in: N servers replicated to one receiver, disjoint dataset trees.**

  * This is the common use case of a centralized backup server.

  * Implementation:

    * N ``push`` jobs (one per sender server), 1 ``sink`` (as long as the different push jobs have a different :ref:`client identity <overview-passive-side--client-identity>`)
    * N ``source`` jobs (one per sender server), N ``pull`` on the receiver server (unique names, disjoint  ``root_fs``)

  * The ``sink`` job automatically constrains each client to a disjoint sub-tree of the sink-side dataset hierarchy ``${root_fs}/${client_identity}``.
    Therefore, the different clients cannot interfere.

  * The ``pull`` job only pulls from one host, so it's up to the zrepl user to ensure that the different ``pull`` jobs don't interfere.

.. _fan-out-replication:

* **Fan-out: 1 server replicated to N receivers**

  * Can be implemented either in a pull or push fashion.

    * **pull setup**: 1 ``pull`` job on each receiver server, each with a corresponding **unique** ``source`` job on the sender server.
    * **push setup**: 1 ``sink`` job on each receiver server, each with a corresponding **unique** ``push`` job on the sender server.

  * It is critical that we have one sending-side job (``source``, ``push``) per receiver.
    The reason is that :ref:`zrepl's ZFS abstractions <zrepl-zfs-abstractions>` (``zrepl zfs-abstraction list``) include the name of the ``source``/``push`` job, but not the receive-side job name or client identity (see :issue:`380`).
    As a counter-example, suppose we used multiple ``pull`` jobs with only one ``source`` job.
    All ``pull`` jobs would share the same :ref:`replication cursor bookmark <replication-cursor-and-last-received-hold>` and trip over each other, breaking incremental replication guarantees quickly.
    The anlogous problem exists for 1 ``push`` to N ``sink`` jobs.

  * The ``filesystems`` matched by the sending side jobs (``source``, ``push``) need not necessarily be disjoint.
    For this to work, we need to avoid interference between snapshotting and pruning of the different sending jobs.
    The solution is to centralize sender-side snapshot management in a separate ``snap`` job.
    Snapshotting in the ``source``/``push`` job should then be disabled (``type: manual``).
    And sender-side pruning (``keep_sender``) needs to be disabled in the active side (``pull`` / ``push``), since that'll be done by the ``snap job``.

  * **Restore limitations**: when restoring from one of the ``pull`` targets (e.g., using ``zfs send -R``), the replication cursor bookmarks don't exist on the restored system.
    This can break incremental replication to all other receive-sides after restore.

  * See :ref:`the fan-out replication quick-start guide <quickstart-fan-out-replication>` for an example of this setup.


**Setups that do not work**:

* N ``pull`` identities, 1 ``source`` job. Tracking :issue:`380`.

