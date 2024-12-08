.. include:: ../global.rst.inc

.. _include:

Including Configuration Files
=============================

It is possible to distribute zrep configurations over multiple YAML configuration
files. This is achieved by using the `include` key which can only exist in the main
configuration file.

The list of included paths must point to either individual YAML files or directories.
If the path points to a directory then all YAML files with a `.yml` extention in the
directory will be included.

::
   global: ...

   include:
      - ./jobs.d/
      - ./more_jobs.d/job.yml


.. NOTE::
   The included path is treated as absolute when starting with `/` else it
   is treated as a relative path that is added to the filepath.Dir() of the
   :ref:`main config file path <overview-terminology>` used by the daemon.


Included configuration files can only specify the following keys:
   - :ref:`jobs <include-jobs>`

.. _include-jobs:

Including Jobs
--------------

Included jobs are defined using the :ref:`same syntax <job>` used in the main
configuration file.

.. NOTE::
   Job names must be unique across all included configuration files.

Examples
^^^^^^^^

Main configuration file:
::
    global: ...
    > /etc/zrepl/zrepl.yml
    include:
      - ./jobs.d/

Included configuration file:
::
    > /etc/zrepl/jobs.d/MyDataSet.yml
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
