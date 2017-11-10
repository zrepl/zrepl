.. include:: ../global.rst.inc
.. _conf-logging:

Logging
=======

zrepl uses structured logging to provide users with easily processable log messages.

Logging outlets are configured in the ``global`` section of the |mainconfig|.
Check out :sampleconf:`random/logging.yml` for an example on how to configure multiple outlets:

::

    global:
      logging:
    
        - outlet: OUTLET_TYPE
          level: MINIMUM_LEVEL
          format: FORMAT
    
        - outlet: OUTLET_TYPE
          level: MINIMUM_LEVEL
          format: FORMAT
    
        ...
    
    jobs: ...

Default Configuration
---------------------

By default, the following logging configuration is used

::

    global:
      logging:
    
        - outlet: "stdout"
          level:  "warn"
          format: "human"

.. ATTENTION::
    Output to **stderr** should always be considered a **critical error**.
    Only errors in the logging infrastructure itself, e.g. IO errors when writing to an outlet, are sent to stderr.

Building Blocks
---------------

The following sections document the semantics of the different log levels, formats and outlet types.

.. _logging-levels:

Levels
~~~~~~

.. list-table::
    :widths: 10 10 80
    :header-rows: 1

    * - Level
      - SHORT
      - Description
    * - ``error``
      - ``ERRO``
      - immediate action required
    * - ``warn``
      - ``WARN``
      - symptoms for misconfiguration, soon expected failure, etc.
    * - ``info``
      - ``INFO``
      - explains what happens without too much detail
    * - ``debug``
      - ``DEBG``
      - tracing information, state dumps, etc. useful for debugging.

Incorrectly classified messages are considered a bug and should be reported.

.. _logging-formats:

Formats
~~~~~~~

.. list-table::
    :widths: 10 90
    :header-rows: 1

    * - Format
      - Description
    * - ``human``
      - emphasizes context by putting job, task, step and other context variables into brackets
        before the actual message, followed by remaining fields in logfmt style|
    * - ``logfmt``
      - `logfmt <https://brandur.org/logfmt>`_ output. zrepl uses `this Go package <https://github.com/go-logfmt/logfmt>`_.
    * - ``json``
      - JSON formatted output. Each line is a valid JSON document. Fields are marshaled by
        ``encoding/json.Marshal()``, which is particularly useful for processing in
        log aggregation or when processing state dumps.

Outlets are ... well ... outlets for log entries into the world.

``stdout`` Outlet
-----------------

.. list-table::
    :widths: 10 90
    :header-rows: 1

    * - Parameter
      - Comment
    * - ``outlet``
      -
    * - ``level``
      -  minimum  :ref:`log level <logging-levels>`
    * - ``format``
      - output :ref:`format <logging-formats>`

Writes all log entries with minimum level ``level`` formatted by ``format`` to stdout.

Can only be specified once.

``syslog`` Outlet
-----------------
.. list-table::
    :widths: 10 90
    :header-rows: 1

    * - Parameter
      - Comment
    * - ``outlet``
      -
    * - ``level``
      -  minimum  :ref:`log level <logging-levels>`
    * - ``format``
      - output :ref:`format <logging-formats>`
    * - ``retry_interval``
      - Interval between reconnection attempts to syslog (default = 0)

Writes all log entries formatted by ``format`` to syslog.
On normal setups, you should not need to change the ``retry_interval``.

Can only be specified once.

``tcp`` Outlet
--------------

.. list-table::
    :widths: 10 90
    :header-rows: 1

    * - Parameter
      - Comment
    * - ``outlet``
      -
    * - ``level``
      -  minimum  :ref:`log level <logging-levels>`
    * - ``format``
      - output :ref:`format <logging-formats>`
    * - ``net``
      - ``tcp`` in most cases
    * - ``address``
      - remote network, e.g. ``logs.example.com:10202``
    * - ``retry_interval``
      - Interval between reconnection attempts to ``address``
    * - ``tls``
      - TLS config (see below)

Establishes a TCP connection to ``address`` and sends log messages with minimum level ``level`` formatted by ``format``.

If ``tls`` is not specified, an unencrypted connection is established.

If ``tls`` is specified, the TCP connection is secured with TLS + Client Authentication.
This is particularly useful in combination with log aggregation services that run on an other machine.

.. list-table::
    :widths: 10 90
    :header-rows: 1

    * - Parameter
      - Description
    * - ``ca``
      - PEM-encoded certificate authority that signed the remote server's TLS certificate
    * - ``cert``
      - PEM-encoded client certificate identifying this zrepl daemon toward the remote server
    * - ``key``
      - PEM-encoded, unencrypted client private key identifying this zrepl daemon toward the remote server

.. NOTE::

    zrepl uses Go's ``crypto/tls`` and ``crypto/x509`` packages and leaves all but the required fields in ``tls.Config`` at their default values.
    In case of a security defect in these packages, zrepl has to be rebuilt because Go binaries are statically linked.
