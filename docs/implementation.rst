Implementation Overview
=======================

.. WARNING::

    Incomplete / under construction

The following design aspects may convince you that `zrepl` is superior to a hacked-together shell script solution.

Testability & Performance
-------------------------

zrepl is written in Go, a real programming language with type safety,
reasonable performance, testing infrastructure and an (opinionated) idea of
software engineering.

* key parts & algorithms of zrepl are covered by unit tests (work in progress)
* zrepl is noticably faster than comparable shell scripts


RPC protocol
------------

While it is tempting to just issue a few `ssh remote 'zfs send ...' | zfs recv`, this has a number of drawbacks:

* The snapshot streams need to be compatible.
* Communication is still unidirectional. Thus, you will most likely
  * either not take advantage of features such as *compressed send & recv*
  * or issue additional `ssh` commands in advance to figure out what features are supported on the other side.
* Advanced logic in shell scripts is ugly to read, poorly testable and a pain to maintain.

zrepl takes a different approach:

* Define an RPC protocol.
* Establish an encrypted, authenticated, bidirectional communication channel...
* ... with zrepl running at both ends of it.

 This has several obvious benefits:

* No blank root shell access is given to the other side.
* Instead, an *authenticated* peer can *request* filesystem lists, snapshot streams, etc.
* Requests are then checked against job-specific ACLs, limiting a client to the filesystems it is actually allowed to replicate.
* The {{< zrepl-transport "transport mechanism" >}} is decoupled from the remaining logic, keeping it extensible.

Protocol Implementation
~~~~~~~~~~~~~~~~~~~~~~~

zrepl implements its own RPC protocol.
This is mostly due to the fact that existing solutions do not provide efficient means to transport large amounts of data.

Package [`github.com/zrepl/zrepl/rpc`](https://github.com/zrepl/zrepl/tree/master/rpc) builds a special-case handling around returning an `io.Reader` as part of a unary RPC call.

Measurements show only a single memory-to-memory copy of a snapshot stream is made using `github.com/zrepl/zrepl/rpc`, and there is still potential for further optimizations.

Logging & Transparency
----------------------

zrepl comes with [rich, structured and configurable logging]({{< relref "configuration/logging.md" >}}), allowing administators to understand what the software is actually doing.
