.. _usage:

*****
Usage
*****

============
CLI Overview
============

.. NOTE::

    To avoid duplication, the zrepl binary is self-documenting:
    invoke any subcommand at any level with the ``--help`` flag to get information on the subcommand, available flags, etc.

.. list-table::
    :widths: 30 70
    :header-rows: 1

    * - Subcommand
      - Description
    * - ``zrepl daemon``
      - run the daemon, required for all zrepl functionality
    * - ``zrepl control``
      - control / query the daemon
    * - ``zrepl control status``
      - show job activity / monitoring (``--format raw``)
    * - ``zrepl test``
      - test configuration, try pattern syntax, dry run pruning policy, etc.
    * - ``zrepl stdinserver``
      - see :ref:`transport-ssh+stdinserver`

.. _usage-zrepl-daemon:

============
zrepl daemon
============

All actual work zrepl does is performed by a daemon process.
Logging is configurable via the config file. Please refer to the :ref:`logging documention <logging>`.

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
