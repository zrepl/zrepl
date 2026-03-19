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
   * - Void linux(sender), Debian(receiving)
     - sink job (receiving)
     - v0.7.0

.. _installation-user-privileges-my-example-setup:

Possible Unpriviliged User Setup
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^

A known tested setup included Void Linux with runit and Debian Bookworm running ``zrepl daemon`` as an unprivileged user,
using the provided unit file running as a non-priviliged ``User=zrepl``.

.. code-block:: bash

   zrepl.service
   ...
   [Unit]
   Description=zrepl daemon
   Documentation=https://zrepl.github.io

   [Service]
   User=zrepl
   Group=zrepl
   Type=simple
   ExecStartPre=/usr/local/bin/zrepl --config /etc/zrepl/zrepl.yml configcheck
   ExecStart=/usr/local/bin/zrepl --config /etc/zrepl/zrepl.yml daemon
   RuntimeDirectory=zrepl zrepl/stdinserver
   RuntimeDirectoryMode=0700

   # Make Go produce coredumps
   Environment=GOTRACEBACK='crash'

   [Install]
   WantedBy=multi-user.target
   ...

The runit service file:

.. code-block:: bash

   zrepl/run
   ...
   #!/bin/sh
   
   # 1. Set Environment Variables
   export GOTRACEBACK='crash'
   
   # 2. Runtime Directory Setup
   # We ensure the directories exist and have strict 0700 permissions
   mkdir -p /var/run/zrepl/stdinserver
   chmod -R 0700 /var/run/zrepl
   chown -R zrepl:zrepl /var/run/zrepl
   
   # 3. Config Check
   # If this fails, we exit immediately so runit doesn't loop-restart a bad config rapidly
   HOME=/nonexistent chpst -u zrepl -U zrepl \
       /usr/local/bin/zrepl --config /etc/zrepl/zrepl.yml configcheck || exit 1
   
   # 4. Start the Daemon
   # MUST use 'exec' to replace the shell process with zrepl
   HOME=/nonexistent exec chpst -u zrepl -U zrepl \
       /usr/local/bin/zrepl --config /etc/zrepl/zrepl.yml daemon
   ...


Tested ZFS permissions are as follows:

.. code-block:: bash

   # receiving side root filesystem
   zfs allow -u zrepl bookmark,create,release,hold,mount,mountpoint,receive,refreservation,userprop backuppool/zrepl
   # sending side
   zfs allow -u zrepl bookmark,release,hold,send prodpool

**Notes:**

* ``snapshot`` is NOT needed for receiving (only for snapshotting operations)
* ``refreservation`` avoids non-sparse volume issues on receiving side according to zfs documentation
* Add ``snapshot`` if your jobs perform sender or receiver-side snapshots
* Add ``destroy`` if your jobs perform sender or receiver-side pruning
* Encryption and other features may require additional permissions (at least ``encryption`` permissions)
* While not tested, ``mountpoint`` permission is most likely only strictly necessary if the ``mountpoint`` itself is to be changed during receive (that is if properties are not also replicated or when overridden/inherited).
* zrepl will report errors and stop running jobs if it cannot create ``bookmarks``.
* ``hold`` is necessary to stop any other process from deleting a snapshot that is currently being replicated. zrepl will not stop if hold is not permitted.
* release is necessary if any process at all is to be able to delete any snapshots after zrepl is done, since the locks need to be lifted.
* zrepl does not function without being able to change ``userprop``.
* According to ZFS documentation and after testing, zfs is indeed unable to receive any dataset without the mount permission (unfortunately, please raise a PR with zfs if you want it to change).

Both mount and mountpoint are the most worrysome permissions that most people will point out, but currently there has not been any alternative solution.
However, it seems zfs is currently unable to mount without being root.
This means that as zrepl is run under the zrepl user, it CAN change the mountpoint arbitrarily (potentially disastrous if filesystems are mounted through other processes with root access) and receive datasets with the mount permission.
What it cannot do, is actually mount the dataset due to kernel restrictions (This is open to change as discussions are already ongoing at ZFS whether or not to fix it, if possible).
