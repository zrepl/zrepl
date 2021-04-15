.. include:: ../global.rst.inc


Conflict Resolution Options
===========================


::

   jobs:
   - type: push
     filesystems: ...
     conflict_resolution:
       initial_replication:
           send_all_snapshots: false # false,true

     ...

.. _conflict_resolution-initial_replication-option-send_all_snapshots:


``send_all_snapshots`` option
-----------------------------

The ``send_all_snapshots`` options control the initial transfer of push and pull jobs.
Left ``false`` (the default), the initial transfer will only transfer the most recent snapshot.
If ``send_all_snapshots`` is ``true`` for the very first transfer of a dataset, all historical snapshots will be transfered.
