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

.. _job-recv-options:

Recv Options
~~~~~~~~~~~~

:ref:`Sink<job-sink>` and :ref:`pull<job-pull>` jobs have an optional ``recv`` configuration section.
However, there are currently no variables to configure there.


