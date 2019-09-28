.. zrepl documentation master file, created by
   sphinx-quickstart on Wed Nov  8 22:28:10 2017.
   You can adapt this file completely to your liking, but it should at least
   contain the root `toctree` directive.

.. include:: global.rst.inc

|GitHub license| |Language: Go| |Twitter| |Donate via Patreon| |Donate via Liberapay| |Donate via PayPal| 


zrepl - ZFS replication
-----------------------

**zrepl** is a one-stop, integrated solution for ZFS replication.

.. raw:: html

   <div style="margin-bottom: 1em; background: #2e3436; color: white; font-size: 0.8em; min-height: 6em; max-width: 100%; overflow: auto;">
     <pre>
     Job: prod_to_backups
     Type: push
     Replication:
         Attempt #1
         Status: fan-out-filesystems
         Progress: [=========================\----] 246.7 MiB / 264.7 MiB @ 11.5 MiB/s
           zroot              STEPPING (step 1/2, 624 B/1.2 KiB) next: @a => @b
           zroot/ROOT         DONE (step 2/2, 1.2 KiB/1.2 KiB)
           zroot/ROOT/default STEPPING (step 1/2, 123.4 MiB/129.3 MiB) next: @a => @b
           zroot/tmp          STEPPING (step 1/2, 29.9 KiB/44.2 KiB) next: @a => @b
           zroot/usr          STEPPING (step 1/2, 624 B/1.2 KiB) next: @a => @b
           zroot/usr/home     STEPPING (step 1/2, 123.3 MiB/135.3 MiB) next: @a => @b
           zroot/var          STEPPING (step 1/2, 624 B/1.2 KiB) next: @a => @b
           zroot/var/audit    DONE (step 2/2, 1.2 KiB/1.2 KiB)
           zroot/var/crash    DONE (step 2/2, 1.2 KiB/1.2 KiB)
           zroot/var/log      STEPPING (step 1/2, 22.0 KiB/29.2 KiB) next: @a => @b
           zroot/var/mail     STEPPING (step 1/2, 624 B/1.2 KiB) next: @a => @b
     Pruning Sender:
         ...
     Pruning Receiver:
     </pre>
   </div>


Getting started
~~~~~~~~~~~~~~~

The :ref:`10 minutes tutorial setup <tutorial>` gives you a first impression.

Main Features
~~~~~~~~~~~~~

* **Filesystem replication**

  * [x] Pull & Push mode
  * [x] Multiple transport :ref:`transports <transport>`: TCP, TCP + TLS client auth, SSH

  * Advanced replication features

    * [x] Automatic retries for temporary network errors
    * [ ] Resumable send & receive
    * [ ] Compressed send & receive
    * [ ] Raw encrypted send & receive

* **Automatic snapshot management**

  * [x] Periodic :ref:`filesystem snapshots <job-snapshotting-spec>`
  * [x] Support for :ref:`pre- and post-snapshot hooks <job-snapshotting-hooks>` with builtins for MySQL & Postgres
  * [x] Flexible :ref:`pruning rule system <prune>`

    * [x] Age-based fading (grandfathering scheme)
    * [x] Bookmarks to avoid divergence between sender and receiver

* **Sophisticated Monitoring & Logging**

  * [x] Live progress reporting via `zrepl status` :ref:`subcommand <usage>`
  * [x] Comprehensive, structured :ref:`logging <logging>`

    * ``human``, ``logfmt`` and ``json`` formatting
    * stdout, syslog and TCP (+TLS client auth) outlets

  * [x] Prometheus :ref:`monitoring <monitoring>` endpoint

* **Maintainable implementation in Go**

  * [x] Cross platform
  * [x] Type safe & testable code


.. ATTENTION::
    zrepl as well as this documentation is still under active development.
    There is no stability guarantee on the RPC protocol or configuration format,
    but we do our best to document breaking changes in the :ref:`changelog`.


Contributing
~~~~~~~~~~~~

We are happy about any help we can get!

* :ref:`Financial Support <supporters>`
* Explore the codebase

  * These docs live in the ``docs/`` subdirectory

* Document any non-obvious / confusing / plain broken behavior you encounter when setting up zrepl for the first time
* Check the *Issues* and *Projects* sections for things to do.
  The `good first issues <https://github.com/zrepl/zrepl/labels/good%20first%20issue>`_ and `docs <https://github.com/zrepl/zrepl/labels/docs>`_ are suitable starting points.

.. admonition:: Development Workflow
    :class: note

    The `GitHub repository <https://github.com/zrepl/zrepl>`_ is where all development happens.
    Make sure to read the `Developer Documentation section <https://github.com/zrepl/zrepl>`_ and open new issues or pull requests there.




Table of Contents
~~~~~~~~~~~~~~~~~

.. toctree::
   :maxdepth: 2
   :caption: Contents:

   tutorial
   installation
   configuration
   usage
   implementation
   pr
   changelog
   GitHub Repository & Issue Tracker <https://github.com/zrepl/zrepl>
   supporters

