.. include:: ../global.rst.inc

.. _quickstart-continuous-replication:

Continuous Backup of a Server
=============================

This config example shows how we can backup our ZFS-based server to another machine using a zrepl push job.

* Production server ``prod`` with filesystems to back up:

  * The entire pool ``zroot``
  * except ``zroot/var/tmp`` and all child datasets of it
  * and except ``zroot/usr/home/paranoid`` which belongs to a user doing backups themselves.

* Backup server ``backups`` with a dataset sub-tree for use by zrepl:

  * In our example, that will be ``storage/zrepl/sink/prod``.

Our backup solution should fulfill the following requirements:

* Periodically snapshot the filesystems on ``prod`` *every 10 minutes*
* Incrementally replicate these snapshots to ``storage/zrepl/sink/prod/*`` on ``backups``
* Keep only very few snapshots on ``prod`` to save disk space
* Keep a fading history (24 hourly, 30 daily, 6 monthly) of snapshots on ``backups``
* The network is untrusted - zrepl should use TLS to protect its communication and our data.

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

Generate TLS Certificates
-------------------------

We use the :ref:`TLS client authentication transport <transport-tcp+tlsclientauth>` to protect our data on the wire.
To get things going quickly, we skip setting up a CA and generate two self-signed certificates as described :ref:`here <transport-tcp+tlsclientauth-2machineopenssl>`.
For convenience, we generate the key pairs on our local machine and distribute them using ssh:

.. code-block:: bash

   (name=backups; openssl req -x509 -sha256 -nodes \
    -newkey rsa:4096 \
    -days 365 \
    -keyout $name.key \
    -out $name.crt -addext "subjectAltName = DNS:$name" -subj "/CN=$name")

   (name=prod; openssl req -x509 -sha256 -nodes \
    -newkey rsa:4096 \
    -days 365 \
    -keyout $name.key \
    -out $name.crt -addext "subjectAltName = DNS:$name" -subj "/CN=$name")

   ssh root@backups "mkdir /etc/zrepl"
   scp  backups.key backups.crt prod.crt root@backups:/etc/zrepl

   ssh root@prod "mkdir /etc/zrepl"
   scp  prod.key prod.crt backups.crt root@prod:/etc/zrepl

Note that alternative transports exist, e.g. via :ref:`TCP without TLS <transport-tcp>` or :ref:`ssh <transport-ssh+stdinserver>`.

Configure server ``prod``
-------------------------

We define a **push job** named ``prod_to_backups`` in ``/etc/zrepl/zrepl.yml`` on host ``prod`` :

.. literalinclude:: ../../internal/config/samples/quickstart_continuous_server_backup_sender.yml

.. _tutorial-configure-prod:

Configure server ``backups``
----------------------------

We define a corresponding **sink job** named ``sink`` in ``/etc/zrepl/zrepl.yml`` on host ``backups`` :

.. literalinclude:: ../../internal/config/samples/quickstart_continuous_server_backup_receiver.yml

Go Back To Quickstart Guide
---------------------------

:ref:`Click here <quickstart-apply-config>` to go back to the quickstart guide.
