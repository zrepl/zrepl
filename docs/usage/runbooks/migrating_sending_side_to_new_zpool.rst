
Migrating Sending Side
~~~~~~~~~~~~~~~~~~~~~~

**Objective**:
Move sending-side zpool to new hardware.
Make the move fully transparent to the sending-side jobs.
After the move is done, all sending-side zrepl jobs should continue to work as if the move had not happened.
In particular, incremental replication should be able to pick up where it left before the move.

Suppose we want to migrate all data from one zpool ``oldpool`` to another zpool ``newpool``.
A possible reason might be that we want to change RAID levels, ``ashift``, or just migrate over to next-gen hardware.

If the pool names are different, zrepl's matching between sender and receiver dataset will break becase the receive-side dataset names contain ``oldpool``.
To avoid this, we will need the name of the new pool to match that of the old pool.
The following steps will accomplish this:

1. Stop zrepl.
2. Create the new pool: ``zpool create newpool ...``
3. Take a snapshot of the old pool so that you have something that you can ``zfs send``.
   For example, run ``zfs snapshot -r oldpool@migration_oldpool_newpool``.
4. Send all of the oldpool's datasets to the new pool:
   ``zfs send -R oldpool@migration_oldpool_newpool | zfs recv -F newpool``
5. Export the old pool: ``zpool export oldpool``
6. Export the new pool: ``zpool export newpool``
7. (Optional) Change the name of the old pool to something that does not conflict with the new pool.
   We are going to use the name ``oldoldpool`` in this example.
   Use ``zpool import`` with no arguments to see the pool id.
   Then ``zpool import <id> oldoldpool && zpool export oldoldpool``.
8. Import the new pool, while changing the name to match the old pool: ``zpool import newpool oldpool``
9. Start zrepl again and wake up the relevant jobs.
10. Use ``zrepl status`` or you monitoring to ensure that replication works.
    The best test is an end-to-end test where you write some junk data on a sender dataset and wait until a snapshot with that data appears on the receiving side.
11. Once you are confident that replication is working, you may dispose of the old pool.

Note that, depending on pruning rules, it will not be possible to switch back to the old pool seamlessly, i.e., without a full re-replication.
