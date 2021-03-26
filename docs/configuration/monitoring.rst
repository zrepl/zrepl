.. include:: ../global.rst.inc

.. _monitoring:

Monitoring
==========

Monitoring endpoints are configured in the ``global.monitoring`` section of the config file.

.. _monitoring-prometheus:

Prometheus & Grafana
--------------------

zrepl can expose `Prometheus metrics <https://prometheus.io/docs/instrumenting/exposition_formats/>`_ via HTTP.
The ``listen`` attribute is a `net.Listen <https://golang.org/pkg/net/#Listen>`_  string for tcp, e.g. ``:9811`` or ``127.0.0.1:9811`` (port 9811 was reserved to zrepl `on the official list <https://github.com/prometheus/prometheus/wiki/Default-port-allocations/_compare/43e495dd251ee328ac0d08b58084665b5c0f7a7e...459195059b55b414193ebeb80c5ba463d2606951>`_).
The ``listen_freebind`` attribute is :ref:`explained here <listen-freebind-explanation>`.
The Prometheus monitoring job appears in the ``zrepl control`` job list and may be specified **at most once**.

zrepl also ships with an importable `Grafana <https://grafana.com>`_ dashboard that consumes the Prometheus metrics:
see :repomasterlink:`dist/grafana`.
The dashboard also contains some advice on which metrics are important to monitor.

.. NOTE::

  At the time of writing, there is no stability guarantee on the exported metrics.

::

    global:
      monitoring:
        - type: prometheus
          listen: ':9811'
          listen_freebind: true # optional, default false



