.. include:: ../global.rst.inc


Send & Recv Options
===================

.. _job-send-options:

Send Options
~~~~~~~~~~~~

::
   
   jobs:
   - type: push
     filesystems: ...
     send:
       encrypted: true
       step_holds:
         disable_incremental: false
     ...

:ref:`Source<job-source>` and :ref:`push<job-push>` jobs have an optional ``send`` configuration section.

``encryption`` option
---------------------

The ``encryption`` variable controls whether the matched filesystems are sent as `OpenZFS native encryption <http://open-zfs.org/wiki/ZFS-Native_Encryption>`_ raw sends.
More specifically, if ``encryption=true``, zrepl

* checks for any of the filesystems matched by ``filesystems`` whether the ZFS ``encryption`` property indicates that the filesystem is actually encrypted with ZFS native encryption and
* invokes the ``zfs send`` subcommand with the ``-w`` option (raw sends) and
* expects the receiving side to support OpenZFS native encryption (recv will fail otherwise)

Filesystems matched by ``filesystems`` that are not encrypted are not sent and will cause error log messages.

If ``encryption=false``, zrepl expects that filesystems matching ``filesystems`` are not encrypted or have loaded encryption keys.


``step_holds.disable_incremental`` option
-----------------------------------------

The ``step_holds.disable_incremental`` variable controls whether the creation of :ref:`step holds <step-holds-and-bookmarks>` should be disabled for incremental replication.
The default value is ``false``.

Disabling step holds has the disadvantage that steps :ref:`might not be resumable <step-holds-and-bookmarks>` if interrupted.
However, the advantage and :issue:`reason for existence <288>` of this flag is that it allows the pruner to delete snapshots of interrupted replication steps
which is useful if replication happens so rarely (or fails so frequently) that the amount of disk space exclusively referenced by the step's snapshots becomes intolerable.

.. _job-recv-options:

Recv Options
~~~~~~~~~~~~

:ref:`Sink<job-sink>` and :ref:`pull<job-pull>` jobs have an optional ``recv`` configuration section.
However, there are currently no variables to configure there.


