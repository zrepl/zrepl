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

.. _usage-zrepl-daemon:

============
zrepl daemon
============

All actual work zrepl does is performed by a daemon process.
The daemon supports structured :ref:`logging <logging>` and provides :ref:`monitoring endpoints <monitoring>`.

When installating from a package, the package maintainer should have provided an init script / systemd.service file.
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

A systemd service defintion template is available in :repomasterlink:`dist/systemd`.
Note that some of the options only work on recent versions of systemd.
Any help & improvements are very welcome, see :issue:`145`.