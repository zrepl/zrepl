.. zrepl documentation master file, created by
   sphinx-quickstart on Wed Nov  8 22:28:10 2017.
   You can adapt this file completely to your liking, but it should at least
   contain the root `toctree` directive.

.. include:: global.rst.inc

zrepl - ZFS replication
-----------------------

.. ATTENTION::
    zrepl as well as this documentation is still under active development.
    It is neither feature complete nor is there a stability guarantee on the configuration format.
    Use & test at your own risk ;)

Getting started
~~~~~~~~~~~~~~~

The :ref:`5 minute tutorial setup <tutorial>` gives you a first impression.

Main Features
~~~~~~~~~~~~~

* Filesystem Replication

  * [x] Local & Remote
  * [x] Pull mode
  * [ ] Push mode
  * [x] Access control checks when pulling datasets
  * [x] :ref:`Flexible mapping <pattern-mapping>` rules
  * [x] Bookmarks support
  * [ ] Feature-negotiation for

    * Resumable `send & receive`
    * Compressed `send & receive`
    * Raw encrypted `send & receive` (as soon as it is available)

* Automatic snapshot creation

  * [x] Ensure fixed time interval between snapshots

* Automatic snapshot :ref:`pruning <prune>`

  * [x] Age-based fading (grandfathering scheme)

* Flexible, detailed & structured :ref:`logging <logging>`

  * [x] ``human``, ``logfmt`` and ``json`` formatting
  * [x] stdout, syslog and TCP (+TLS client auth) outlets

* Maintainable implementation in Go

  * [x] Cross platform
  * [x] Type safe & testable code

Contributing
~~~~~~~~~~~~

We are happy about any help we can get!

* Explore the codebase

  * These docs live in the ``docs/`` subdirectory

* Document any non-obvious / confusing / plain broken behavior you encounter when setting up zrepl for the first time
* Check the *Issues* and *Projects* sections for things to do

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
   implementation
   changelog
   GitHub Repository & Issue Tracker <https://github.com/zrepl/zrepl>
   pr
