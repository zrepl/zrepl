.. _miscellaneous:

Miscellaneous
=============

.. _conf-runtime-directories:

Runtime Directories & UNIX Sockets
----------------------------------

The zrepl daemon needs to open various UNIX sockets in a runtime directory:

* a ``control`` socket that the CLI commands use to interact with the daemon
* the :ref:`transport-ssh+stdinserver` listener opens one socket per configured client, named after ``client_identity`` parameter

There is no authentication on these sockets except the UNIX permissions.
The zrepl daemon will refuse to bind any of the above sockets in a directory that is world-accessible.

The following sections of the ``global`` config shows the default paths.
The shell script below shows how the default runtime directory can be created.

::

    global:
      control:
        sockpath: /var/run/zrepl/control
      serve:
        stdinserver:
          sockdir: /var/run/zrepl/stdinserver


::

    mkdir -p /var/run/zrepl/stdinserver
    chmod -R 0700 /var/run/zrepl


Durations & Intervals
---------------------

Interval & duration fields in job definitions, pruning configurations, etc. must match the following regex:

::

    var durationStringRegex *regexp.Regexp = regexp.MustCompile(`^\s*(\d+)\s*(s|m|h|d|w)\s*$`)
    // s = second, m = minute, h = hour, d = day, w = week (7 days)
