
.. _configuration_preface:

=======
Preface
=======

-----------------------
Configuration File Path
-----------------------

zrepl searches for its main configuration file in the following locations (in that order):

* If set, the location specified via the global ``--config`` flag
* ``/etc/zrepl/zrepl.yml``
* ``/usr/local/etc/zrepl/zrepl.yml``

The examples in the :ref:`tutorial` or the :sampleconf:`/` directory should provide a good starting point.

-------------------
Runtime Directories
-------------------

zrepl requires runtime directories for various UNIX sockets --- they are documented in the :ref:`config file<conf-runtime-directories>`.
Your package maintainer / init script should take care of creating them.
Alternatively, for default settings, the following should to the trick.

::

    mkdir -p /var/run/zrepl/stdinserver
    chmod -R 0700 /var/run/zrepl


----------
Validating
----------

The config can be validated using the ``zrepl configcheck`` subcommand.

