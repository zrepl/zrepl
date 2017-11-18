.. _binary releases: https://github.com/zrepl/zrepl/releases

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

zrepl source releases are signed & tagged by the author in the git repository.
Your OS vendor may provide binary packages of zrepl through the package manager.
Additionally, `binary releases`_ are provided on GitHub.
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
      - Use `binary releases`_ or build from source.

Compile From Source
~~~~~~~~~~~~~~~~~~~

Producing a release requires **Go 1.9** or newer and **Python 3** + **pip3** + ``docs/requirements.txt`` for the Sphinx documentation.
A tutorial to install Go is available over at `golang.org <https://golang.org/doc/install>`_.
Python and pip3 should probably be installed via your distro's package manager.

Alternatively, you can use the Docker build process:
it is used to produce the official zrepl `binary releases`_
and serves as a reference for build dependencies and procedure:

::

    git clone https://github.com/zrepl/zrepl.git
    cd zrepl
    sudo docker build -t zrepl_build -f build.Dockerfile .
    sudo docker run -it --rm \
        -v "${PWD}:/zrepl" \
        --user "$(id -u):$(id -g)" \
        zrepl_build make release

Alternatively, you can install build dependencies on your local system and then build in your ``$GOPATH``:

::

    mkdir -p "${GOPATH}/src/github.com/zrepl/zrepl"
    git clone https://github.com/zrepl/zrepl.git "${GOPATH}/src/github.com/zrepl/zrepl"
    cd "${GOPATH}/src/github.com/zrepl/zrepl"
    ./lazy.sh devsetup
    make release

Build results are located in the ``artifacts/`` directory.

.. NOTE::

    It is your job to install the apropriate binary in the zrepl users's ``$PATH``, e.g. ``/usr/local/bin/zrepl``.
    Otherwise, the examples in the :ref:`tutorial` may need to be adjusted.

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
