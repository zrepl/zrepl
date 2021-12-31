.. include:: ../global.rst.inc

.. _quickstart-fan-out-replication:

Fan-out replication
===================

This quick-start example demonstrates how to implement a fan-out replication setup where datasets on a server (A) are replicated to multiple targets (B, C, etc.).

This example uses multiple ``source`` jobs on server A and ``pull`` jobs on the target servers.

.. WARNING::

   Before implementing this setup, please see the caveats listed in the :ref:`fan-out replication configuration overview <fan-out-replication>`.

Overview
--------

On the source server (A), there should be:

* A ``snap`` job

  * Creates the snapshots
  * Handles the pruning of snapshots

* A ``source`` job for target B

  * Accepts connections from server B and B only

* Further ``source`` jobs for each additional target (C, D, etc.)

  * Listens on a unique port
  * Only accepts connections from the specific target

On each target server, there should be:

* A ``pull`` job that connects to the corresponding ``source`` job on A

  * ``prune_sender`` should keep all snapshots since A's ``snap`` job handles the pruning
  * ``prune_receiver`` can be configured as appropriate on each target server

Generate TLS Certificates
-------------------------

Mutual TLS via the :ref:`TLS client authentication transport <transport-tcp+tlsclientauth>` can be used to secure the connections between the servers. In this example, a self-signed certificate is created for each server without setting up a CA.

.. code-block:: bash

    source=a.example.com
    targets=(
        b.example.com
        c.example.com
        # ...
    )

    for server in "${source}" "${targets[@]}"; do
        openssl req -x509 -sha256 -nodes \
            -newkey rsa:4096 \
            -days 365 \
            -keyout "${server}.key" \
            -out "${server}.crt" \
            -addext "subjectAltName = DNS:${server}" \
            -subj "/CN=${server}"
    done

    # Distribute each host's keypair
    for server in "${source}" "${targets[@]}"; do
        ssh root@"${server}" mkdir /etc/zrepl
        scp "${server}".{crt,key} root@"${server}":/etc/zrepl/
    done

    # Distribute target certificates to the source
    scp "${targets[@]/%/.crt}" root@"${source}":/etc/zrepl/

    # Distribute source certificate to the targets
    for server in "${targets[@]}"; do
        scp "${source}.crt" root@"${server}":/etc/zrepl/
    done

Configure source server A
-------------------------

.. literalinclude:: ../../config/samples/quickstart_fan_out_replication_source.yml

Configure each target server
----------------------------

.. literalinclude:: ../../config/samples/quickstart_fan_out_replication_target.yml

Go Back To Quickstart Guide
---------------------------

:ref:`Click here <quickstart-apply-config>` to go back to the quickstart guide.
