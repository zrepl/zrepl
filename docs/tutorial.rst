.. include:: global.rst.inc

.. _tutorial:

Tutorial
========


This tutorial shows how zrepl can be used to implement a ZFS-based pull backup.
We assume the following scenario:

* Production server ``prod`` with filesystems to back up:

  * ``zroot/var/db``
  * ``zroot/usr/home`` and all its child filesystems
  * **except** ``zroot/usr/home/paranoid`` belonging to a user doing backups themselves

* Backup server ``backups`` with

  * Filesystem ``storage/zrepl/pull/prod`` + children dedicated to backups of ``prod``

Our backup solution should fulfill the following requirements:

* Periodically snapshot the filesystems on ``prod`` *every 10 minutes*
* Incrementally replicate these snapshots to ``storage/zrepl/pull/prod/*`` on ``backups``
* Keep only very few snapshots on ``prod`` to save disk space
* Keep a fading history (24 hourly, 30 daily, 6 monthly) of snapshots on ``backups``

Analysis
--------

We can model this situation as two jobs:

* A **source job** on ``prod``

  * Creates the snapshots
  * Keeps a short history of snapshots to enable incremental replication to ``backups``
  * Accepts connections from ``backups``

* A **pull job** on ``backups``

  * Connects to the ``zrepl daemon`` process on ``prod``
  * Pulls the snapshots to ``storage/zrepl/pull/prod/*``
  * Fades out snapshots in ``storage/zrepl/pull/prod/*`` as they age


Why doesn't the **pull job** create the snapshots before pulling?

As is the case with all distributed systems, the link between ``prod`` and ``backups`` might be down for an hour or two.
We do not want to sacrifice our required backup resolution of 10 minute intervals for a temporary connection outage.

When the link comes up again, ``backups`` will catch up with the snapshots taken by ``prod`` in the meantime, without a gap in our backup history.

Install zrepl
-------------

Follow the :ref:`OS-specific installation instructions <installation>` and come back here.

Configure server ``backups``
----------------------------

We define a **pull job** named ``pull_prod`` in ``/etc/zrepl/zrepl.yml`` or ``/usr/local/etc/zrepl/zrepl.yml`` on host ``backups`` : ::

    jobs:
    - name: pull_prod
      type: pull
      connect:
        type: tcp
        address: "192.168.2.20:2342"
      root_fs: "storage/zrepl/pull/prod"
      interval: 10m
      pruning:
        keep_sender:
        - type: not_replicated
        - type: last_n
          count: 10
        keep_receiver:
        - type: grid
          grid: 1x1h(keep=all) | 24x1h | 30x1d | 6x30d
          regex: "^zrepl_"
          interval: 10m

The ``connect`` section instructs the zrepl daemon to use plain TCP transport.
Check out the :ref:`transports <transport>` section for alternatives that support encryption.

.. _tutorial-configure-prod:

Configure server ``prod``
-------------------------

We define a corresponding **source job** named ``source_backups`` in ``/etc/zrepl/zrepl.yml`` or ``/usr/local/etc/zrepl/zrepl.yml`` on host ``prod`` : ::

    jobs:
    - name: source_backups
      type: source
      serve:
        type: tcp
        listen: ":2342"
        clients: {
          "192.168.2.10" : "backups"
        }
      filesystems: {
        "zroot/var/db:": true,
        "zroot/usr/home<": true,
        "zroot/usr/home/paranoid": false
      }
      snapshotting:
        type: periodic
        prefix: zrepl_
        interval: 10m


The ``serve`` section whitelists ``backups``'s IP address ``192.168.2.10`` and assigns it the client identity ``backups`` which will show up in the logs.
Again, check the :ref:`docs for encrypted transports <transport>`.

Apply Configuration Changes
---------------------------

We need to restart the zrepl daemon on **both** ``prod`` and ``backups``.
This is :ref:`OS-specific <usage-zrepl-daemon-restarting>`.

Watch it Work
-------------

Run ``zrepl status`` on ``prod`` to monitor the replication and pruning activity.

Additionally, you can check the detailed structured logs of the `zrepl daemon` process and use GNU *watch* to view the snapshots present on both machines.

If you like tmux, here is a handy script that works on FreeBSD: ::

    pkg install gnu-watch tmux
    tmux new-window
    tmux split-window "tail -f /var/log/zrepl.log"
    tmux split-window "gnu-watch 'zfs list -t snapshot -o name,creation -s creation | grep zrepl_'"
    tmux select-layout tiled

The Linux equivalent might look like this: ::

    # make sure tmux is installed & let's assume you use systemd + journald
    tmux new-window
    tmux split-window "journalctl -f -u zrepl.service"
    tmux split-window "watch 'zfs list -t snapshot -o name,creation -s creation | grep zrepl_'"
    tmux select-layout tiled

Summary
-------

Congratulations, you have a working pull backup. Where to go next?

* Read more about :ref:`configuration format, options & job types <configuration_toc>`
* Configure :ref:`logging <logging>` \& :ref:`monitoring <monitoring>`.
* Learn about :ref:`implementation details <implementation_toc>` of zrepl.


