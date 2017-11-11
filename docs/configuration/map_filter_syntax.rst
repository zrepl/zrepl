.. include:: ../global.rst.inc

Mapping & Filter Syntax
=======================

For various job types, a filesystem ``mapping`` or ``filter`` needs to be
specified.

Both have in common that they take a filesystem path (in the ZFS filesystem hierarchy)as parameters and return something.
Mappings return a *target filesystem* and filters return a *filter result*.

The pattern syntax is the same for mappings and filters and is documented in the following section.

Common Pattern Syntax
---------------------

A mapping / filter is specified as a **YAML dictionary** with patterns as keys and
results as values.
The following rules determine which result is chosen for a given filesystem path:

* More specific path patterns win over less specific ones
* Non-wildcard patterns (full path patterns) win over *subtree wildcards* (`<` at end of pattern)

The **subtree wildcard** ``<`` means "the dataset left of ``<`` and all its children".

Example
~~~~~~~

::

    # Rule number and its pattern
    1: tank<            # tank and all its children
    2: tank/foo/bar     # full path pattern (no wildcard)
    3: tank/foo<        # tank/foo and all its children
    
    # Which rule applies to given path?
    tank/foo/bar/loo => 3
    tank/bar         => 1
    tank/foo/bar     => 2
    zroot            => NO MATCH
    tank/var/log     => 1

.. _pattern-mapping:

Mappings
--------

Mappings map a *source filesystem path* to a *target filesystem path*.
Per pattern, either a target filesystem path or ``"!"`` is specified as a result.

* If no pattern matches, there exists no target filesystem (``NO MATCH``).
* If the result is a ``"!"``, there exists no target filesystem (``NO MATCH``).
* If the pattern is a non-wildcard pattern, the source path is mapped to the target path on the right.
* If the pattern ends with a *subtree wildcard* (``<``), the source path is **prefix-trimmed** with the path specified left of ``<``.

  * Note: this means that only for *wildcard-only* patterns (pattern= ``<`` ) is the source path simply appended to the target path.

The example is from the :sampleconf:`localbackup/host1.yml` example config.

::

    jobs:
    - name: mirror_local
      type: local
      mapping: {
        "zroot/var/db<":    "storage/backups/local/zroot/var/db",
        "zroot/usr/home<":  "storage/backups/local/zroot/usr/home",
        "zroot/usr/home/paranoid":  "!", #don't backup paranoid user
        "zroot/poudriere/ports<": "!", #don't backup the ports trees
      }
      ...


::

    zroot/var/db                        => storage/backups/local/zroot/var/db
    zroot/var/db/a/child                => storage/backups/local/zroot/var/db/a/child
    zroot/usr/home                      => storage/backups/local/zroot/usr/home
    zroot/usr/home/paranoid             => NOT MAPPED
    zroot/usr/home/bob                  => storage/backups/local/zroot/usr/home/bob
    zroot/usr/src                       => NOT MAPPED
    zroot/poudriere/ports/2017Q3        => NOT MAPPED
    zroot/poudriere/ports/HEAD          => NOT MAPPED

.. _pattern-filter:

Filters
-------

Valid filter results: ``ok`` or ``!``.

The example below show the source job from the :ref:`tutorial <tutorial-configure-app-srv>`:
The corresponding pull job is allowed access to ``zroot/var/db``, ``zroot/usr/home`` + children except ``zroot/usr/home/paranoid``::

    jobs:
    - name: pull_backup
      type: source
      ...
      filesystems: {
        "zroot/var/db": "ok",
        "zroot/usr/home<": "ok",
        "zroot/usr/home/paranoid": "!",
      }
      ...
