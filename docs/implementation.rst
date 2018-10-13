.. _implementation_toc:

Implementation Overview
=======================

.. WARNING::

   Incomplete and possibly outdated.
   Check out the :ref:`talks about zrepl <pr-talks>` at various conferences for up-to-date material.
   Alternatively, have a `look at the source code <http://github.com/zrepl/zrepl>`_ ;)

The following design aspects may convince you that ``zrepl`` is superior to a hacked-together shell script solution.
Also check out the :ref:`talks about zrepl <pr-talks>` at various conferences.

Testability & Performance
-------------------------

zrepl is written in Go, a real programming language with type safety,
reasonable performance, testing infrastructure and an (opinionated) idea of
software engineering.

* key parts & algorithms of zrepl are covered by unit tests (work in progress)
* zrepl is noticably faster than comparable shell scripts


RPC protocol
------------

While it is tempting to just issue a few ``ssh remote 'zfs send ...' | zfs recv``, this has a number of drawbacks:

* The snapshot streams need to be compatible.
* Communication is still unidirectional. Thus, you will most likely

  * either not take advantage of advanced replication features such as *compressed send & recv*
  * or issue additional ``ssh`` commands in advance to figure out what features are supported on the other side.

* Advanced logic in shell scripts is ugly to read, poorly testable and a pain to maintain.

zrepl takes a different approach:

* Define an RPC protocol.
* Establish an encrypted, authenticated, bidirectional communication channel.
* Run daemons on both sides of the setup and let them talk to each other.

This has several obvious benefits:

* No blank root shell access is given to the other side.
* An *authenticated* peer *requests* filesystem lists, snapshot streams, etc.
* The server decides which filesystems it exposes to which peers.
* The :ref:`transport mechanism <transport>` is decoupled from the remaining logic, which allows us to painlessly offer multiple transport mechanisms.

Protocol Implementation
~~~~~~~~~~~~~~~~~~~~~~~

zrepl uses a custom RPC protol because, at the time of writing, existing solutions like gRPC do not provide efficient means to transport large amounts of data, whose size is unknown at send time (= zfs send streams).
The package used is `github.com/problame/go-streamrpc <https://github.com/problame/go-streamrpc/tree/master>`_.

Logging & Transparency
----------------------

zrepl comes with :ref:`rich, structured and configurable logging <logging>`, allowing administators to understand what the software is actually doing.
