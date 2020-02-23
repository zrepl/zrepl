.. include:: ../global.rst.inc

.. _logging:

Logging
=======

zrepl uses structured logging to provide users with easily processable log messages.

Logging outlets are configured in the ``global`` section of the config file.

::

    global:
      logging:
    
        - type: OUTLET_TYPE
          level: MINIMUM_LEVEL
          format: FORMAT
    
        - type: OUTLET_TYPE
          level: MINIMUM_LEVEL
          format: FORMAT
    
        ...
    
    jobs: ...

.. _logging-error-outlet:

.. ATTENTION::
    The **first outlet is special**: if an error writing to any outlet occurs, the first outlet receives the error and can print it.
    Thus, the first outlet must be the one that always works and does not block, e.g. ``stdout``, which is the default.

.. _logging-default-config:

Default Configuration
---------------------

By default, the following logging configuration is used

::

    global:
      logging:
    
        - type: "stdout"
          level:  "warn"
          format: "human"

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
      - prints job and subsystem into brackets before the actual message,
        followed by remaining fields in logfmt style
    * - ``logfmt``
      - `logfmt <https://brandur.org/logfmt>`_ output. zrepl uses `this Go package <https://github.com/go-logfmt/logfmt>`_.
    * - ``json``
      - JSON formatted output. Each line is a valid JSON document. Fields are marshaled by
        ``encoding/json.Marshal()``, which is particularly useful for processing in
        log aggregation or when processing state dumps.

Outlets
~~~~~~~

Outlets are the destination for log entries.

.. _logging-outlet-stdout:

``stdout`` Outlet
-----------------

.. list-table::
    :widths: 10 90
    :header-rows: 1

    * - Parameter
      - Comment
    * - ``type``
      - ``stdout``
    * - ``level``
      -  minimum  :ref:`log level <logging-levels>`
    * - ``format``
      - output :ref:`format <logging-formats>`
    * - ``time``
      - always include time in output (``true`` or ``false``)
    * - ``color``
      - colorize output according to log level (``true`` or ``false``)

Writes all log entries with minimum level ``level`` formatted by ``format`` to stdout.
If stdout is a tty, interactive usage is assumed and both ``time`` and ``color`` are set to ``true``.

Can only be specified once.

``syslog`` Outlet
-----------------
.. list-table::
    :widths: 10 90
    :header-rows: 1

    * - Parameter
      - Comment
    * - ``type``
      - ``syslog``
    * - ``level``
      -  minimum  :ref:`log level <logging-levels>`
    * - ``format``
      - output :ref:`format <logging-formats>`
    * - ``facility``
      - Which syslog facility to use (default = ``local0``)
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
    * - ``type``
      - ``tcp``
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
The latter is particularly useful in combination with log aggregation services.

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

.. WARNING::

    zrepl drops log messages to the TCP outlet if the underlying connection is not fast enough.
    Note that TCP buffering in the kernel must first run full before messages are dropped.

    Make sure to always configure a ``stdout`` outlet as the special error outlet to be informed about problems
    with the TCP outlet (see :ref:`above <logging-error-outlet>` ).


.. NOTE::

    zrepl uses Go's ``crypto/tls`` and ``crypto/x509`` packages and leaves all but the required fields in ``tls.Config`` at their default values.
    In case of a security defect in these packages, zrepl has to be rebuilt because Go binaries are statically linked.
