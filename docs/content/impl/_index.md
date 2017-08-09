+++
title = "Implementation Overview"
+++

The following design aspects may convince you that `zrepl` is superior to a hacked-together shell script solution.

## Language

`zrepl` is written in Go, a real programming language with type safety,
reasonable performance, testing infrastructure and an (opinionated) idea of
software engineering.

* key parts & algorithms of `zrepl` are covered by unit tests
* zrepl is noticably faster than comparable shell scripts


## RPC protocol

While it is tempting to just issue a few `ssh remote 'zfs send ...' | zfs recv`, this has a number of drawbacks:

* The snapshot streams need to be compatible.
* Communication is still unidirectional. Thus, you will most likely
    * either not take advantage of features such as *compressed send & recv*
    * or issue additional `ssh` commands in advance to figure out what features are supported on the other side.
* Advanced logic in shell scripts is ugly to read, poorly testable and a pain to maintain.

`zrepl` takes a different approach:

* Define an RPC protocol.
* Establish an encrypted, authenticated, bidirectional communication channel...
* ... with `zrepl` running at both ends of it.

 This has several obvious benefits:

* No blank root shell access is given to the other side.
* Instead, access control lists (ACLs) are used to grant permissions to *authenticated* peers. 
* The transport mechanism is decoupled from the remaining logic, keeping it extensible (e.g. TCP+TLS)

{{% panel %}}
Currently, the bidirectional communication channel is multiplexed on top of a single SSH connection.
Local replication is of course handled efficiently via simple method calls
See TODO for details.
{{% / panel %}}

