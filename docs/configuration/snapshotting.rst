.. include:: ../global.rst.inc

.. _job-snapshotting-spec:

Taking Snaphots
===============

You can configure zrepl to take snapshots of the filesystems in the ``filesystems`` field specified in ``push``, ``source`` and ``snap`` jobs.

The following snapshotting types are supported:


.. list-table::
    :widths: 20 70
    :header-rows: 1

    * - ``snapshotting.type``
      - Comment
    * - ``periodic``
      - Ensure that snapshots are taken at a particular interval.
    * - ``cron``
      - Use cron spec to take snapshots at particular points in time.
    * - ``manual``
      - zrepl does not take any snapshots by itself.

The ``periodic`` and ``cron`` snapshotting types share some common options and behavior:

* **Naming:** The snapshot names are composed of a user-defined ``prefix`` followed by a UTC date formatted like ``20060102_150405_000``.
  We use UTC because it will avoid name conflicts when switching time zones or between summer and winter time.
* **Hooks:** You can configure hooks to run before or after zrepl takes the snapshots. See :ref:`below <job-snapshotting-hooks>` for details.
* **Push replication:** After creating all snapshots, the snapshotter will wake up the replication part of the job, if it's a ``push`` job.
  Note that snapshotting is decoupled from replication, i.e., if it is down or takes too long, snapshots will still be taken.
  Note further that other jobs are not woken up by snapshotting.

.. NOTE::

   There is **no concept of ownership** of the snapshots that are created by ``periodic`` or ``cron``.
   Thus, there is no distinction between zrepl-created snapshots and user-created snapshots during replication or pruning.

   In particular, pruning will take all snapshots into consideration by default.
   To constrain pruning to just zrepl-created snapshots:

     1. Assign a unique `prefix` to the snapshotter and
     2. Use the ``regex`` functionality of the various pruning ``keep`` rules to just consider snapshots with that prefix.

  There is currently no way to constrain replication to just zrepl-created snapshots.
  Follow and comment at :issue:`403` if you need this functionality.

.. NOTE::

  The ``zrepl signal wakeup JOB`` subcommand does not trigger snapshotting.

``periodic`` Snapshotting
-------------------------

::

   jobs:
   - ...
     filesystems: { ... }
     snapshotting:
       type: periodic
       prefix: zrepl_
       interval: 10m
       hooks: ...
    pruning: ...

The ``periodic`` snapshotter ensures that snapshots are taken in the specified ``interval``.
If you use zrepl for backup, this translates into your recovery point objective (RPO).
To meet your RPO, you still need to monitor that replication, which happens asynchronously to snapshotting, actually works.

It is desirable to get all ``filesystems`` snapshotted simultaneously because it results in a more consistent backup.
To accomplish this while still maintaining the ``interval``, the ``periodic`` snapshotter attempts to get the snapshotting rhythms in sync.
To find that sync point, the most recent snapshot, created by the snapshotter, in any of the matched ``filesystems`` is used.
A filesystem that does not have snapshots by the snapshotter has lower priority than filesystem that do, and thus might not be snapshotted (and replicated) until it is snapshotted at the next sync point.
The snapshotter uses the ``prefix`` to identify which snapshots it created.

.. _job-snapshotting--cron:

``cron`` Snapshotting
---------------------

::

   jobs:
   - type: snap
     filesystems: { ... }
     snapshotting:
       type: cron
       prefix: zrepl_
       # (second, optional) minute hour day-of-month month day-of-week
       # This example takes snapshots daily at 3:00.
       cron: "0 3 * * *"
     pruning: ...

In ``cron`` mode, the snapshotter takes snaphots at fixed points in time.
See https://en.wikipedia.org/wiki/Cron for details on the syntax.
zrepl uses the ``the github.com/robfig/cron/v3`` Go package for parsing.
An optional field for "seconds" is supported to take snapshots at sub-minute frequencies.

``manual`` Snapshotting
-----------------------

::

   jobs:
   - type: push
     snapshotting:
       type: manual
     ...

In ``manual`` mode, zrepl does not take snapshots by itself.
Manual snapshotting is most useful if you have existing infrastructure for snapshot management.
Or, if you want to decouple snapshot management from replication using a zrepl ``snap`` job.
See :ref:`this quickstart guide <quickstart-backup-to-external-disk>` for an example.

To trigger replication after taking snapshots, use the ``zrepl signal wakeup JOB`` command.

.. _job-snapshotting-hooks:

Pre- and Post-Snapshot Hooks
----------------------------

Jobs with `periodic snapshots <job-snapshotting-spec_>`_ can run hooks before and/or after taking the snapshot specified in ``snapshotting.hooks``:
Hooks are called per filesystem before and after the snapshot is taken (pre- and post-edge).
Pre-edge invocations are in configuration order, post-edge invocations in reverse order, i.e. like a stack.
If a pre-snapshot invocation fails, ``err_is_fatal=true`` cuts off subsequent hooks, does not take a snapshot, and only invokes post-edges corresponding to previous successful pre-edges.
``err_is_fatal=false`` logs the failed pre-edge invocation but does not affect subsequent hooks nor snapshotting itself.
Post-edges are only invoked for hooks whose pre-edges ran without error.
Note that hook failures for one filesystem never affect other filesystems.

The optional ``timeout`` parameter specifies a period after which zrepl will kill the hook process and report an error.
The default is 30 seconds and may be specified in any units understood by `time.ParseDuration <https://golang.org/pkg/time/#ParseDuration>`_.

The optional ``filesystems`` filter which limits the filesystems the hook runs for. This uses the same |filter-spec| as jobs.

Most hook types take additional parameters, please refer to the respective subsections below.

.. list-table::
    :widths: 20 10 70
    :header-rows: 1

    * - Hook ``type``
      - Details
      - Description
    * - ``command``
      - :ref:`Details <job-hook-type-command>`
      - Arbitrary pre- and post snapshot scripts.
    * - ``postgres-checkpoint``
      - :ref:`Details <job-hook-type-postgres-checkpoint>`
      - Execute Postgres ``CHECKPOINT`` SQL command before snapshot.
    * - ``mysql-lock-tables``
      - :ref:`Details <job-hook-type-mysql-lock-tables>`
      - Flush and read-Lock MySQL tables while taking the snapshot.
      
.. _job-hook-type-command:

``command`` Hooks
~~~~~~~~~~~~~~~~~

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
        hooks:
        - type: command
          path: /etc/zrepl/hooks/zrepl-notify.sh
          timeout: 30s
          err_is_fatal: false
        - type: command
          path: /etc/zrepl/hooks/special-snapshot.sh
          filesystems: {
            "tank/special": true
          }
      ...


``command`` hooks take a ``path`` to an executable script or binary to be executed before and after the snapshot.
``path`` must be absolute (e.g. ``/etc/zrepl/hooks/zrepl-notify.sh``).
No arguments may be specified; create a wrapper script if zrepl must call an executable that requires arguments.
The process standard output is logged at level INFO. Standard error is logged at level WARN.
The following environment variables are set:

* ``ZREPL_HOOKTYPE``: either "pre_snapshot" or "post_snapshot"
* ``ZREPL_FS``: the ZFS filesystem name being snapshotted
* ``ZREPL_SNAPNAME``: the zrepl-generated snapshot name (e.g. ``zrepl_20380119_031407_000``)
* ``ZREPL_DRYRUN``: set to ``"true"`` if a dry run is in progress so scripts can print, but not run, their commands

An empty template hook can be found in :sampleconf:`/hooks/template.sh`.

.. _job-hook-type-postgres-checkpoint:

``postgres-checkpoint`` Hook
~~~~~~~~~~~~~~~~~~~~~~~~~~~~

Connects to a Postgres server and executes the ``CHECKPOINT`` statement pre-snapshot.
Checkpointing applies the WAL contents to all data files and syncs the data files to disk.
This is not required for a consistent database backup: it merely forward-pays the "cost" of WAL replay to the time of snapshotting instead of at restore.
However, the Postgres manual recommends against checkpointing during normal operation.
Further, the operation requires Postgres superuser privileges.
zrepl users must decide on their own whether this hook is useful for them (it likely isn't).

.. ATTENTION::
    Note that WALs and Postgres data directory (with all database data files) must be on the same filesystem to guarantee a correct point-in-time backup with the ZFS snapshot.

DSN syntax documented here: `<https://godoc.org/github.com/lib/pq>`_

.. code-block:: sql

   CREATE USER zrepl_checkpoint PASSWORD yourpasswordhere;
   ALTER ROLE zrepl_checkpoint SUPERUSER;

.. code-block:: yaml

  - type: postgres-checkpoint
    dsn: "host=localhost port=5432 user=postgres password=yourpasswordhere sslmode=disable"
    filesystems: {
        "p1/postgres/data11": true
    }

.. _job-hook-type-mysql-lock-tables:

``mysql-lock-tables`` Hook
~~~~~~~~~~~~~~~~~~~~~~~~~~

Connects to MySQL and executes

* pre-snapshot ``FLUSH TABLES WITH READ LOCK`` to lock all tables in all databases in the MySQL server we connect to (`docs <https://dev.mysql.com/doc/refman/8.0/en/flush.html#flush-tables-with-read-lock>`_)
* post-snapshot ``UNLOCK TABLES``  reverse above operation.

Above procedure is documented in the `MySQL manual <https://dev.mysql.com/doc/mysql-backup-excerpt/5.7/en/backup-methods.html>`_
as a means to produce a consistent backup of a MySQL DBMS installation (i.e., all databases).

`DSN syntax <https://github.com/go-sql-driver/mysql#dsn-data-source-name>`_: ``[username[:password]@][protocol[(address)]]/dbname[?param1=value1&...&paramN=valueN]``

.. ATTENTION::
    All MySQL databases must be on the same ZFS filesystem to guarantee a consistent point-in-time backup with the ZFS snapshot.

.. code-block:: sql

   CREATE USER zrepl_lock_tables IDENTIFIED BY 'yourpasswordhere';
   GRANT RELOAD ON *.* TO zrepl_lock_tables;
   FLUSH PRIVILEGES;

.. code-block:: yaml

  - type: mysql-lock-tables
    dsn: "zrepl_lock_tables:yourpasswordhere@tcp(localhost)/"
    filesystems: {
      "tank/mysql": true
    }
