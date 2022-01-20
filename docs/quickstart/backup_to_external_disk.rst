.. include:: ../global.rst.inc

.. _quickstart-backup-to-external-disk:

Local Snapshots + Offline Backup to an External Disk
====================================================

This config example shows how we can use zrepl to make periodic snapshots of our local workstation and back it up to a zpool on an external disk which we occassionally connect.

The local snapshots should be taken every 15 minutes for pain-free recovery from CLI disasters (``rm -rf /`` and the like).
However, we do not want to keep the snapshots around for very long because our workstation is a little tight on disk space.
Thus, we only keep one hour worth of high-resolution snapshots, then fade them out to one per hour for a day (24 hours), then one per day for 14 days.

At the end of each work day, we connect our external disk that serves as our workstation's local offline backup.
We want zrepl to inspect the filesystems and snapshots on the external pool, figure out which snapshots were created since the last time we connected the external disk, and use incremental replication to efficiently mirror our workstation to our backup disk.
Afterwards, we want to clean up old snapshots on the backup pool: we want to keep all snapshots younger than one hour, 24 for each hour of the first day, then 360 daily backups.

A few additional requirements:

* Snapshot creation and pruning on our workstation should happen in the background, without interaction from our side.
* However, we want to explicitly trigger replication via the command line.
* We want to use OpenZFS native encryption to protect our data on the external disk.
  It is absolutely critical that only encrypted data leaves our workstation.
  **zrepl should provide an easy config knob for this and prevent replication of unencrypted datasets to the external disk.**
* We want to be able to put off the backups for more than three weeks, i.e., longer than the lifetime of the automatically created snapshots on our workstation.
  **zrepl should use bookmarks and holds to achieve this goal**.
* When we yank out the drive during replication and go on a long vacation, we do *not* want the partially replicated snapshot to stick around as it would hold on to too much disk space over time.
  Therefore, we want zrepl to deviate from its :ref:`default behavior <replication-option-protection>` and sacrifice resumability, but nonetheless retain the ability to do incremental replication once we return from our vacation.
  **zrepl should provide an easy config knob to disable step holds for incremental replication**.

The following config snippet implements the setup described above.
You will likely want to customize some aspects mentioned in the top comment in the file.

.. literalinclude:: ../../config/samples/quickstart_backup_to_external_disk.yml

:ref:`Click here <quickstart-apply-config>` to go back to the quickstart guide.
