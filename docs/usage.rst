*****
Usage
*****

============
CLI Overview
============

.. NOTE::

    To avoid duplication, the zrepl binary is self-documenting:
    invoke any subcommand at any level with the ``--help`` flag to get information on the subcommand, available flags, etc.

.. list-table::
    :widths: 30 70
    :header-rows: 1

    * - Subcommand
      - Description
    * - ``zrepl daemon``
      - run the daemon, required for all zrepl functionality
    * - ``zrepl control``
      - control / query the daemon
    * - ``zrepl control status``
      - show job activity / monitoring (``--format raw``)
    * - ``zrepl stdinserver``
      - see :ref:`transport-ssh+stdinserver`

