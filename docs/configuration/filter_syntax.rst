.. include:: ../global.rst.inc

.. _pattern-filter:

Filter Syntax
=============

For :ref:`source<job-source>`, :ref:`push<job-push>` and :ref:`snap<job-snap>` jobs, a filesystem filter must be defined (field ``filesystems``).
A filter takes a filesystem path (in the ZFS filesystem hierarchy) as parameter and returns ``true`` (pass) or ``false`` (block).

A filter is specified as a **YAML dictionary** with patterns as keys and booleans as values.
The following rules determine which result is chosen for a given filesystem path:

* More specific path patterns win over less specific ones
* Non-wildcard patterns (full path patterns) win over *subtree wildcards* (`<` at end of pattern)
* If the path in question does not match any pattern, the result is ``false``.

The **subtree wildcard** ``<`` means "the dataset left of ``<`` and all its children".
   
.. TIP::
  You can try out patterns for a configured job using the ``zrepl test filesystems`` subcommand for push and source jobs.

Examples
--------

Full Access
~~~~~~~~~~~

The following configuration will allow access to all filesystems.

::

   jobs:
   - type: source
     filesystems: {
       "<": true,
     }
     ...


Fine-grained
~~~~~~~~~~~~

The following configuration demonstrates all rules presented above.

::

    jobs:
    - type: source
      filesystems: {
        "tank<": true,          # rule 1
        "tank/foo<": false,     # rule 2
        "tank/foo/bar": true,  # rule 3
      }
      ...


Which rule applies to given path, and what is the result?

::

    tank/foo/bar/loo => 2    false
    tank/bar         => 1    true
    tank/foo/bar     => 3    true
    zroot            => NONE false
    tank/var/log     => 1    true

