.. include:: ../global.rst.inc

.. _monitoring:

Monitoring
==========

Monitoring endpoints are configured in the ``global.monitoring`` section of the |mainconfig|.
Check out :sampleconf:`random/logging_and_monitoring.yml` for examples.

.. _monitoring-prometheus:

Prometheus
----------

zrepl can expose `Prometheus metrics <https://prometheus.io/docs/instrumenting/exposition_formats/>`_ via HTTP.
The ``listen`` attribute is a `net.Listen <https://golang.org/pkg/net/#Listen>`_  string for tcp, e.g. ``:9091`` or ``127.0.0.1:9091``.

The Prometheues monitoring job appears in the ``zrepl control`` job list and may be specified **at most once**.
There is no stability guarantee on the exported metrics.

::

    global:
      monitoring:
        - type: prometheus
          listen: ':9091'



