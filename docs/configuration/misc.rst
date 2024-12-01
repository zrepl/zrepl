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


.. _conf-include-directories:

Including Configuration Directories
-----------------------------------

It is possible to distribute zrepl job definitions over multiple YAML files. This is
achieved by using the `include` key in the configuration file.

The directive is mutually exclusive with any other jobs definition and can only exist
in the configuration file:

::
   global: ...

   jobs:
     include: jobs.d

.. NOTE::
   The included directory path is treated as absolute when starting with `/` else it
   is treated as a relative path from the directory of the loaded configuration file.

Included YAML job files must end with the `.yml` extension and can only contain
contain the `jobs` key. Additionally, job names must be unique across all job YAML
files.

Examples
^^^^^^^^

::
  > /etc/zrepl/zrepl.yml

   jobs:
     include: jobs.d

  > /etc/zrepl/zrepl.d/MyDataSet.yml
   jobs:
   - name: snapjob
     type: snap
     filesystems: {
       "MyPool/MyDataset<": true,
     }
     snapshotting:
       type: periodic
       interval: 2m
       prefix: zrepl_snapjob_
     pruning:
       keep:
         - type: last_n
           count: 60
