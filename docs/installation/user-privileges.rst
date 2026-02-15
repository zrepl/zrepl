.. _installation-user-privileges:

User Privileges
---------------

zrepl can run as an unprivileged user with `ZFS delegation <https://www.freebsd.org/doc/handbook/zfs-zfs-allow.html>`_.
**Help us document working setups on this page** by opening a PR!

.. NOTE::

   Keep in mind that ``zfs send``/``recv`` was never designed with
   untrusted input in mind. An attacker controlling the send-recv stream could probably crash the
   receive-side kernel, exploit bugs to get code execution, or induce stateful damage to the receive-side pool.


Known Working Setups
^^^^^^^^^^^^^^^^^^^^

.. list-table::
   :header-rows: 1
   :widths: 15 40 20

   * - OS
     - Use Case
     - Last Tested Version
   * - Linux
     - sink job (receiving)
     - v0.7.0

.. _installation-user-privileges-my-example-setup:

My Example Setup
^^^^^^^^^^^^^^^^

I'm on Linux (Ubuntu ...) and run ``zrepl daemon`` as an unprivileged user, using a custom systemd unit file.

:: code-block:: bash
   ...
   [Service]
   User=zrepl
   Group=zrepl
   ...

I set up the ZFS persmissions as follows:

.. code-block:: bash

   # receiving side root filesystem
   zfs allow -u zrepl bookmark,create,destroy,hold,mount,mountpoint,receive,refreservation,userprop backuppool/zrepl
   # sending side
   zfs allow -u zrepl bookmark,destroy,hold,send,userprop prodpool

**Notes:**

* ``snapshot`` is NOT needed for receiving (only for pruning operations)
* ``refreservation`` avoids non-sparse volume issues on receiving side
* Add ``snapshot`` if your jobs perform sender or receiver-side pruning
* Encryption and other features may require additional permissions
