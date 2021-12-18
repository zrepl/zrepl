.. _usage:

*****
Usage
*****

============
CLI Overview
============

.. NOTE::

    The zrepl binary is self-documenting:
    run ``zrepl help`` for an overview of the available subcommands or ``zrepl SUBCOMMAND --help`` for information on available flags, etc.

.. _cli-signal-wakeup:

.. list-table::
    :widths: 30 70
    :header-rows: 1

    * - Subcommand
      - Description
    * - ``zrepl help``
      - show subcommand overview
    * - ``zrepl daemon``
      - run the daemon, required for all zrepl functionality
    * - ``zrepl status``
      - show job activity, or with ``--raw`` for JSON output
    * - ``zrepl stdinserver``
      - see :ref:`transport-ssh+stdinserver`
    * - ``zrepl signal wakeup JOB``
      - manually trigger replication + pruning of JOB
    * - ``zrepl signal reset JOB``
      - manually abort current replication + pruning of JOB
    * - ``zrepl configcheck``
      - check if config can be parsed without errors
    * - ``zrepl migrate``
      - | perform on-disk state / ZFS property migrations
        | (see :ref:`changelog <changelog>` for details)
    * - ``zrepl zfs-abstraction``
      - list and remove zrepl's abstractions on top of ZFS, e.g. holds and step bookmarks (see :ref:`overview <replication-cursor-and-last-received-hold>` )

.. _usage-zrepl-daemon:

============
zrepl daemon
============

All actual work zrepl does is performed by a daemon process.
The daemon supports structured :ref:`logging <logging>` and provides :ref:`monitoring endpoints <monitoring>`.

When installing from a package, the package maintainer should have provided an init script / systemd.service file.
You should thus be able to start zrepl daemon using your init system.

Alternatively, or for running zrepl in the foreground, simply execute ``zrepl daemon``.
Note that you won't see much output with the :ref:`default logging configuration<logging-default-config>`:

.. ATTENTION::

    Make sure to actually monitor the error level output of zrepl: some configuration errors will not make the daemon exit.

    Example: if the daemon cannot create the :ref:`transport-ssh+stdinserver` sockets in the runtime directory,
    it will emit an error message but not exit because other tasks such as periodic snapshots & pruning are of equal importance.

.. _usage-zrepl-daemon-restarting:

Restarting
~~~~~~~~~~

The daemon handles SIGINT and SIGTERM for graceful shutdown.
Graceful shutdown means at worst that a job will not be rescheduled for the next interval.
The daemon exits as soon as all jobs have reported shut down.

Systemd Unit File
~~~~~~~~~~~~~~~~~

A systemd service definition template is available in :repomasterlink:`dist/systemd`.
Note that some of the options only work on recent versions of systemd.
Any help & improvements are very welcome, see :issue:`145`.



============
Ops Runbooks
============


.. toctree::

   usage/runbooks/migrating_sending_side_to_new_zpool.rst


==============
Platform Tests
==============

Along with the main ``zrepl`` binary, we release the ``platformtest`` binaries.
The zrepl platform tests are an integration test suite that is complementary to the pure Go unit tests.
Any test that needs to interact with ZFS is a platform test.

The platform need to run as root.
For each test, we create a fresh dummy zpool backed by a file-based vdev.
The file path, and a root mountpoint for the dummy zpool, must be specified on the command line:

::

   mkdir -p /tmp/zreplplatformtest
   ./platformtest \
       -poolname 'zreplplatformtest' \  # <- name must contain zreplplatformtest
       -imagepath /tmp/zreplplatformtest.img \ # <- zrepl will create the file
       -mountpoint /tmp/zreplplatformtest # <- must exist


.. WARNING::

   ``platformtest`` will unconditionally overwrite the file at `imagepath`
   and unconditionally ``zpool destroy $poolname``.
   So, don't use a production poolname, and consider running the test in a VM.
   It'll be a lot faster as well because the underlying operations, ``zfs list`` in particular, will be faster.


While the platformtests are running, there will be a log of log output.
After all tests have run, it prints a summary with a list of tests, grouped by result type (success, failure, skipped):

::

   PASSING TESTS:
     github.com/zrepl/zrepl/platformtest/tests.BatchDestroy
     github.com/zrepl/zrepl/platformtest/tests.CreateReplicationCursor
     github.com/zrepl/zrepl/platformtest/tests.GetNonexistent
     github.com/zrepl/zrepl/platformtest/tests.HoldsWork
     ...
     github.com/zrepl/zrepl/platformtest/tests.SendStreamNonEOFReadErrorHandling
     github.com/zrepl/zrepl/platformtest/tests.UndestroyableSnapshotParsing
   SKIPPED TESTS:
     github.com/zrepl/zrepl/platformtest/tests.SendArgsValidationEncryptedSendOfUnencryptedDatasetForbidden__EncryptionSupported_false
   FAILED TESTS: []


If there is a failure, or a skipped test that you believe should be passing, re-run the test suite, capture stderr & stdout to a text file, and create an issue on GitHub.

To run a specific test case, or a subset of tests matched by regex, use the ``-run REGEX`` command line flag.

To stop test execution at the first failing test, and prevent cleanup of the dummy zpool, use the ``-failure.stop-and-keep-pool`` flag.

To build the platformtests yourself, use ``make test-platform-bin``.
There's also the ``make test-platform`` target to run the platform tests with a default command line.
