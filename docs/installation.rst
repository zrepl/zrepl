.. _installation:

Installation
============

.. TIP::

    Note: check out the :ref:`tutorial` if you want a first impression of zrepl.

User Privileges
---------------

It is possible to run zrepl as an unprivileged user in combination with
`ZFS delegation <https://www.freebsd.org/doc/handbook/zfs-zfs-allow.html>`_.
Also, there is the possibility to run it in a jail on FreeBSD by delegating a dataset to the jail.
However, until we get around documenting those setups, you will have to run zrepl as root or experiment yourself :)

Packages
--------

zrepl releases are signed & tagged by the author in the git repository.
Your OS vendor may provide binary packages of zrepl through the package manager.
The following list may be incomplete, feel free to submit a PR with an update:

.. list-table::
    :header-rows: 1

    * - OS / Distro
      - Install Command
      - Link
    * - FreeBSD
      - ``pkg install zrepl``
      - `<https://www.freshports.org/sysutils/zrepl/>`_
    * - Others
      -
      - Install from source, see below

Compile From Source
~~~~~~~~~~~~~~~~~~~

Check out the sources yourself, fetch dependencies using ``dep``, compile and install to the zrepl user's ``$PATH``.
You may want to switch to a tagged commit (we use `semver <http://semver.org>`_) although a checkout of ``master`` branch should generally work.
**Note**: if the zrepl binary is not in ``$PATH``, the examples in the :ref:`tutorial` may need to be adjusted.

::

    # NOTE: you may want to checkout & build as an unprivileged user
    cd /root
    git clone https://github.com/zrepl/zrepl.git
    cd zrepl
    dep ensure
    go build -o zrepl
    cp zrepl /usr/local/bin/zrepl
    rehash
    # see if it worked
    zrepl help

.. _mainconfigfile:

Configuration Files
-------------------

zrepl searches for its main configuration file in the following locations (in that order):

* ``/etc/zrepl/zrepl.yml``
* ``/usr/local/etc/zrepl/zrepl.yml``

Alternatively, use CLI flags to specify a config location.
Copy a config from the :ref:`tutorial` or the ``cmd/sampleconf`` directory to one of these locations and customize it to your setup.

Runtime Directories
-------------------

zrepl requires ephemeral runtime directories where control sockets, etc are placed.
Refer to the :ref:`configuration documentation <conf-runtime-directories>` for more information.

When installing from a package, the package maintainer should have taken care of setting them up through the init system.
Alternatively, for default settings, the following should to the trick.

::

    mkdir -p /var/run/zrepl/stdinserver
    chmod -R 0700 /var/run/zrepl


Running the Daemon
------------------

All actual work zrepl does is performed by a daemon process.
Logging is configurable via the config file. Please refer to the :ref:`logging documention <logging>`.

When installating from a package, the package maintainer should have provided an init script / systemd.service file.
You should thus be able to start zrepl daemon using your init system.

Alternatively, or for running zrepl in the foreground, simply execute ``zrepl daemon``.
Note that you won't see any output unless you configure :ref:`stdout logging outlet <logging-outlet-stdout>`.

.. ATTENTION::

    Make sure to actually monitor the error level output of zrepl: some configuration errors will not make the daemon exit.

    Example: if the daemon cannot create the :ref:`transport-ssh+stdinserver` sockets in the runtime directory,
    it will emit an error message but not exit because other tasks such as periodic snapshots & pruning are of equal importance.

.. _install-restarting:

Restarting
~~~~~~~~~~

The daemon handles SIGINT and SIGTERM for graceful shutdown.
Graceful shutdown means at worst that a job will not be rescheduled for the next interval.
The daemon exits as soon as all jobs have reported shut down.
