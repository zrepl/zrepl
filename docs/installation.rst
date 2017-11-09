.. _installation:

Installation
============

.. TIP::

    Note: check out the [tutorial]({{< relref "tutorial/_index.md" >}}) if you want a first impression of zrepl.

User Privileges
---------------

It is possible to run zrepl as an unprivileged user in combination with
[ZFS delegation](https://www.freebsd.org/doc/handbook/zfs-zfs-allow.html).

Also, there is the possibility to run it in a jail on FreeBSD by delegating a dataset to the jail.

However, until we get around documenting those setups, you will have to run zrepl as root or experiment yourself :)

Installation
------------

zrepl is currently not packaged on any operating system. Signed & versioned releases are planned but not available yet.

Check out the sources yourself, fetch dependencies using dep, compile and install to the zrepl user's `$PATH`.<br />
**Note**: if the zrepl binary is not in `$PATH`, you will have to adjust the examples in the [tutorial]({{< relref "tutorial/_index.md" >}}).

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

* `/etc/zrepl/zrepl.yml`
* `/usr/local/etc/zrepl/zrepl.yml`

Alternatively, use CLI flags to specify a config location.

Copy a config from the [tutorial]({{< relref "tutorial/_index.md" >}}) or the `cmd/sampleconf` directory to one of these locations and customize it to your setup.

## Runtime Directories

Check the the [configuration documentation]({{< relref "configuration/misc.md#runtime-directories-unix-sockets" >}}) for more information.
For default settings, the following should to the trick.

```bash
mkdir -p /var/run/zrepl/stdinserver
chmod -R 0700 /var/run/zrepl
```


Running the Daemon
------------------

All actual work zrepl does is performed by a daemon process.

Logging is configurable via the config file. Please refer to the [logging documentation]({{< relref "configuration/logging.md" >}}).

::

    zrepl daemon

There are no *rc(8)* or *systemd.service(5)* service definitions yet. Note the *daemon(8)* utility on FreeBSD.

.. ATTENTION::

    Make sure to actually monitor the error level output of zrepl: some configuration errors will not make the daemon exit.<br />
    Example: if the daemon cannot create the [stdinserver]({{< relref "configuration/transports.md#stdinserver" >}}) sockets
    in the runtime directory, it will emit an error message but not exit because other tasks such as periodic snapshots & pruning are of equal importance.

.. _install-restarting:

Restarting
~~~~~~~~~~

The daemon handles SIGINT and SIGTERM for graceful shutdown.

Graceful shutdown means at worst that a job will not be rescheduled for the next interval.

The daemon exits as soon as all jobs have reported shut down.
