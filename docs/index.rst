.. zrepl documentation master file, created by
   sphinx-quickstart on Wed Nov  8 22:28:10 2017.
   You can adapt this file completely to your liking, but it should at least
   contain the root `toctree` directive.

.. include:: global.rst.inc

|GitHub license| |Language: Go| |User Docs| |Donate via PayPal| |Donate
via Liberapay| |Twitter|

.. |GitHub license| image:: https://img.shields.io/github/license/zrepl/zrepl.svg
   :target: https://github.com/zrepl/zrepl/blob/master/LICENSE
.. |Language: Go| image:: https://img.shields.io/badge/language-Go-6ad7e5.svg
   :target: https://golang.org/
.. |User Docs| image:: https://img.shields.io/badge/docs-web-blue.svg
   :target: https://zrepl.github.io
.. |Donate via PayPal| image:: https://img.shields.io/badge/donate-paypal-yellow.svg
   :target: https://www.paypal.com/cgi-bin/webscr?cmd=_s-xclick&hosted_button_id=R5QSXJVYHGX96
.. |Donate via Liberapay| image:: https://img.shields.io/badge/donate-liberapay-yellow.svg
   :target: https://liberapay.com/zrepl/donate
.. |Twitter| image:: https://img.shields.io/twitter/url/https/github.com/zrepl/zrepl.svg?style=social
   :target: https://twitter.com/intent/tweet?text=Wow:&url=https%3A%2F%2Fgithub.com%2Fzrepl%2Fzrepl

zrepl - ZFS replication
-----------------------

**zrepl** is a one-stop, integrated solution for ZFS replication.

.. raw:: html

   <div style="margin-bottom: 1em; background: #2e3436; min-height: 6em; max-width: 100%">
     <a href="https://raw.githubusercontent.com/wiki/zrepl/zrepl/zrepl_0.1_status.mp4" target="_new" >
       <video title="zrepl status subcommand" loop autoplay style="width: 100%; display: block;" src="https://raw.githubusercontent.com/wiki/zrepl/zrepl/zrepl_0.1_status.mp4"></video>
     </a>
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

  * [x] Periodic filesystem snapshots
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

