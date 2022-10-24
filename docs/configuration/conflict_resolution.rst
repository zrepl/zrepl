.. include:: ../global.rst.inc


Conflict Resolution Options
===========================


::

   jobs:
   - type: push
     filesystems: ...
     conflict_resolution:
       initial_replication: most_recent | all | fail # default: most_recent

     ...

.. _conflict_resolution-initial_replication:


``initial_replication`` option
------------------------------

The ``initial_replication`` option determines how many snapshots zrepl replicates if the filesystem has not been replicated before.
If ``most_recent`` (the default), the initial replication will only transfer the most recent snapshot, while ignoring previous snapshots.
If all snapshots should be replicated, specify ``all``.
Use ``fail`` to make replication of the filesystem fail in case there is no corresponding fileystem on the receiver.

For example, suppose there are snapshosts ``tank@1``, ``tank@2``, ``tank@3`` on a sender.
Then ``most_recent`` will replicate just ``@3``, but ``all`` will replicate ``@1``, ``@2``, and ``@3``.

If initial replication is interrupted, and there is at least one (maybe partial) snapshot on the receiver, zrepl will always resume in **incremental mode**.
And that is regardless of where the initial replication was interrupted.

For example, if ``initial_replication: all`` and the transfer of ``@1`` is interrupted, zrepl would retry/resume at ``@1``.
And even if the user changes the config to ``initial_replication: most_recent`` before resuming, **incremental mode** will still resume at ``@1``.
