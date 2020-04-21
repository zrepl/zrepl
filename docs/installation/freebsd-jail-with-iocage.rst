.. include:: ../global.rst.inc

.. _installation-freebsd-jail-with-iocage:

FreeBSD Jail With iocage
========================


This tutorial shows how zrepl can be installed on FreeBSD, or FreeNAS in a jail using iocage.
While this tutorial focuses on using iocage, much of the setup would be similar
using a different jail manager.

.. NOTE::

   From a security perspective, just keep in mind that ``zfs send``/``recv`` was never designed with
   jails in mind, an attacker could probably crash the receive-side kernel or worse induce stateful
   damage to the receive-side pool if they were able to get access to the jail.

   The jail doesn't provide security benefits, but only management ones.

Requirements
------------

A dataset that will be delegated to the jail needs to be created if one does not already exist.
For the tutorial ``tank/zrepl`` will be used.

.. code-block:: bash

   zfs create -o mountpoint=none tank/zrepl

The only software requirements on the host system are ``iocage``, which can be installed
from ports or packages.

.. code-block:: bash

   pkg install py37-iocage

.. NOTE::

   By default ``iocage`` will "activate" on first use which will set up some defaults such as
   which pool will be used. To activate ``iocage`` manually the ``iocage activate`` command can be used.

Jail Creation
-------------

There are two options for jail creation using FreeBSD.

1. Manually set up the jail from scratch
2. Create the jail using the ``zrepl`` plugin. On FreeNAS this is possible from the user interface using the community index.

Manual Jail
###########

Create a jail, using the same release as the host, called ``zrepl`` that will be automatically started at boot.
The jail will have ``tank/zrepl`` delegated into it.

.. code-block:: bash

   iocage create --release "$(freebsd-version -k | cut -d '-' -f '1,2')" --name zrepl \
          boot=on nat=1 \
          jail_zfs=on \
          jail_zfs_dataset=zrepl \
          jail_zfs_mountpoint='none'

Enter the jail:

.. code-block:: bash

   iocage console zrepl

Install ``zrepl``

.. code-block:: bash

   pkg update && pkg upgrade
   pkg install zrepl

Create the log file ``/var/log/zrepl.log``

.. code-block:: bash

   touch /var/log/zrepl.log && service newsyslog restart

Tell syslogd to redirect facility local0 to the ``zrepl.log`` file:

.. code-block:: bash

   service syslogd reload

Enable the zrepl daemon to start automatically at boot:

.. code-block:: bash

   sysrc zrepl_enable="YES"


Plugin
######

When using the plugin, ``zrepl`` will be installed for you in a jail using the following ``iocage`` properties.

* ``nat=1``
* ``jail_zfs=on``
* ``jail_zfs_mountpoint=none``

Additionally the delegated dataset should be specified upon creation, and optionally start on boot can be set.
This can also be done from the FreeNAS webui.

.. code-block:: bash

   fetch https://raw.githubusercontent.com/ix-plugin-hub/iocage-plugin-index/master/zrepl.json -o /tmp/zrepl.json
   iocage fetch -P /tmp/zrepl.json --name zrepl jail_zfs_dataset=zrepl boot=on

Configuration
-------------

Now ``zrepl`` can be configured.

Enter the jail.

.. code-block:: bash

   iocage console zrepl

Modify the ``/usr/local/etc/zrepl/zrepl.yml`` configuration file.

.. TIP::

    Note: check out the :ref:`tutorial` for examples of a ``sink`` job.

Now ``zrepl`` can be started.

.. code-block:: bash

   service zrepl start

Summary
-------

Congratulations, you have a working jail!
