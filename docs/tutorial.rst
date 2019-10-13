.. include:: global.rst.inc

.. _tutorial:

Tutorial
========


This tutorial shows how zrepl can be used to implement a ZFS-based push backup.
We assume the following scenario:

* Production server ``prod`` with filesystems to back up:

  * ``zroot/var/db``
  * ``zroot/usr/home`` and all its child filesystems
  * **except** ``zroot/usr/home/paranoid`` belonging to a user doing backups themselves

* Backup server ``backups`` with

  * Filesystem ``storage/zrepl/sink/prod`` + children dedicated to backups of ``prod``

Our backup solution should fulfill the following requirements:

* Periodically snapshot the filesystems on ``prod`` *every 10 minutes*
* Incrementally replicate these snapshots to ``storage/zrepl/sink/prod/*`` on ``backups``
* Keep only very few snapshots on ``prod`` to save disk space
* Keep a fading history (24 hourly, 30 daily, 6 monthly) of snapshots on ``backups``

Analysis
--------

We can model this situation as two jobs:

* A **push job** on ``prod``

  * Creates the snapshots
  * Keeps a short history of local snapshots to enable incremental replication to ``backups``
  * Connects to the ``zrepl daemon`` process on ``backups``
  * Pushes snapshots ``backups``
  * Prunes snapshots on ``backups`` after replication is complete

* A **sink job** on ``backups``

  * Accepts connections & responds to requests from ``prod``
  * Limits client ``prod`` access to filesystem sub-tree ``storage/zrepl/sink/prod``

Install zrepl
-------------

Follow the :ref:`OS-specific installation instructions <installation>` and come back here.

Generate TLS Certificates
-------------------------

We use the `TLS client authentication transport <transport-tcp+tlsclientauth>` to protect our data on the wire.
To get things going quickly, we skip setting up a CA and generate two self-signed certificates as described :ref:`here <transport-tcp+tlsclientauth-2machineopenssl>`.
Again, for convenience, We generate the key pairs on our local machine and distribute them using ssh:

.. code-block:: bash
   :emphasize-lines: 6,13

   openssl req -x509 -sha256 -nodes \
      -newkey rsa:4096 \
      -days 365 \
      -keyout backups.key \
      -out backups.crt
   # ... and use "backups" as Common Name (CN)

   openssl req -x509 -sha256 -nodes \
      -newkey rsa:4096 \
      -days 365 \
      -keyout prod.key \
      -out prod.crt
   # ... and use "prod" as Common Name (CN)

   ssh root@backups "mkdir /etc/zrepl"
   scp  backups.key backups.crt prod.crt root@backups:/etc/zrepl

   ssh root@prod "mkdir /etc/zrepl"
   scp  prod.key prod.crt backups.crt root@prod:/etc/zrepl


Configure server ``prod``
-------------------------

We define a **push job** named ``prod_to_backups`` in ``/etc/zrepl/zrepl.yml`` on host ``prod`` : ::

    jobs:
    - name: prod_to_backups
      type: push 
      connect:
        type: tls
        address: "backups.example.com:8888"
        ca: /etc/zrepl/backups.crt
        cert: /etc/zrepl/prod.crt
        key:  /etc/zrepl/prod.key
        server_cn: "backups"
      filesystems: {
        "zroot/var/db": true,
        "zroot/usr/home<": true,
        "zroot/usr/home/paranoid": false
      }
      snapshotting:
        type: periodic
        prefix: zrepl_
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

.. _tutorial-configure-prod:

Configure server ``backups``
----------------------------

We define a corresponding **sink job** named ``sink`` in ``/etc/zrepl/zrepl.yml`` on host ``prod`` : ::

    jobs:
    - name: sink
      type: sink 
      serve:
         type: tls
         listen: ":8888"
         ca: "/etc/zrepl/prod.crt"
         cert: "/etc/zrepl/backups.crt"
         key: "/etc/zrepl/backups.key"
         client_cns:
           - "prod"
      root_fs: "storage/zrepl/sink"


Apply Configuration Changes
---------------------------

We use ``zrepl configcheck`` before to catch any configuration errors: no output indicates that everything is fine.
If that is the case, restart the zrepl daemon on **both** ``prod`` and ``backups`` using ``service zrepl restart`` or ``systemctl restart zrepl``.


Watch it Work
-------------

Run ``zrepl status`` on ``prod`` to monitor the replication and pruning activity.
To re-trigger replication (snapshots are separate!), use ``zrepl signal wakeup prod_to_backups`` on ``prod``.

If you like tmux, here is a handy script that works on FreeBSD: ::

    pkg install gnu-watch tmux
    tmux new -s zrepl -d
    tmux split-window -t zrepl "tail -f /var/log/messages"
    tmux split-window -t zrepl "gnu-watch 'zfs list -t snapshot -o name,creation -s creation | grep zrepl_'"
    tmux split-window -t zrepl "zrepl status"
    tmux select-layout -t zrepl tiled
    tmux attach -t zrepl

The Linux equivalent might look like this: ::

    # make sure tmux is installed & let's assume you use systemd + journald
    tmux new -s zrepl -d
    tmux split-window -t zrepl  "journalctl -f -u zrepl.service"
    tmux split-window -t zrepl "watch 'zfs list -t snapshot -o name,creation -s creation | grep zrepl_'"
    tmux split-window -t zrepl "zrepl status"
    tmux select-layout -t zrepl tiled
    tmux attach -t zrepl

Summary
-------

Congratulations, you have a working push backup. Where to go next?

* Read more about :ref:`configuration format, options & job types <configuration_toc>`
* Configure :ref:`logging <logging>` \& :ref:`monitoring <monitoring>`.

