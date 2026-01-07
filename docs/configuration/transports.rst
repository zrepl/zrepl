.. highlight:: bash

.. _transport:

Transports
==========

The zrepl RPC layer uses **transports** to establish a single, bidirectional data stream between an active and passive job.
On the passive (serving) side, the transport also provides the **client identity** to the upper layers:
this string is used for access control and separation of filesystem sub-trees in :ref:`sink jobs <job-sink>`.
Transports are specified in the ``connect`` or ``serve`` section of a job definition.

.. contents::

.. ATTENTION::

    The **client identities must be valid ZFS dataset path components**
    because the :ref:`sink job <job-sink>` uses ``${root_fs}/${client_identity}`` to determine the client's subtree.

.. _transport-tcp:

``tcp`` Transport
-----------------

The ``tcp`` transport uses plain TCP, which means that the data is **not encrypted** on the wire.
Clients are identified by their IPv4 or IPv6 addresses, and the client identity is established through a mapping on the server.

This transport may also be used in conjunction with network-layer encryption and/or VPN tunnels to provide encryption on the wire.
To make the IP-based client authentication effective, such solutions should provide authenticated IP addresses.
Some options to consider:

.. _transport-tcp-tunneling:

* `WireGuard <https://www.wireguard.com/>`_: Linux-focussed, in-kernel TLS
* `OpenVPN <https://openvpn.net/>`_: Cross-platform VPN, uses tun on \*nix
* `IPSec <https://en.wikipedia.org/wiki/IPsec>`_: Properly standardized, in-kernel network-layer VPN
* `spiped <http://www.tarsnap.com/spiped.html>`_: think of it as an encrypted pipe between two servers
* SSH

  * `sshuttle <https://sshuttle.readthedocs.io/en/stable/overview.html>`_: VPN-like solution, but using SSH
  * `SSH port forwarding <https://help.ubuntu.com/community/SSH/OpenSSH/PortForwarding>`_: Systemd user unit & make it start before the zrepl service.

Serve
~~~~~

::

    jobs:
    - type: sink
      serve:
        type: tcp
        listen: ":8888"
        listen_freebind: true # optional, default false
        clients: {
          "192.168.122.123" :               "mysql01",
          "192.168.122.42" :                "mx01",
          "2001:0db8:85a3::8a2e:0370:7334": "gateway",

          # CIDR masks require a '*' in the client identity string
          # that is expanded to the client's IP address

          "10.23.42.0/24":       "cluster-*"
          "fde4:8dba:82e1::/64": "san-*"
        }
      ...

.. _listen-freebind-explanation:

``listen_freebind`` controls whether the socket is allowed to bind to non-local or unconfigured IP addresses (Linux ``IP_FREEBIND`` , FreeBSD ``IP_BINDANY``).
Enable this option if you want to ``listen`` on a specific IP address that might not yet be configured when the zrepl daemon starts.

Connect
~~~~~~~

::

    jobs:
     - type: push
       connect:
         type: tcp
         address: "10.23.42.23:8888"
         dial_timeout: # optional, default 10s
       ...

.. _transport-tcp+tlsclientauth:

``tls`` Transport
-----------------

The ``tls`` transport uses TCP + TLS with client authentication using client certificates.
The client identity is the common name (CN) presented in the client certificate.

It is recommended to set up a dedicated CA infrastructure for this transport, e.g. using OpenVPN's `EasyRSA <https://github.com/OpenVPN/easy-rsa>`_.
For a simple 2-machine setup, mutual TLS might also be sufficient.
We provide :ref:`copy-pastable instructions to generate the certificates below <transport-tcp+tlsclientauth-certgen>`.

The implementation uses `Go's TLS library <https://golang.org/pkg/crypto/tls/>`_.
Since Go binaries are statically linked, you or your distribution need to recompile zrepl when vulnerabilities in that library are disclosed.

All file paths are resolved relative to the zrepl daemon's working directory.
Specify absolute paths if you are unsure what directory that is (or find out from your init system).

If intermediate CAs are used, the **full chain** must be present in either in the ``ca`` file or the individual ``cert`` files.
Regardless, the client's certificate must be first in the ``cert`` file, with each following certificate directly certifying the one preceding it (see `TLS's specification <https://tools.ietf.org/html/rfc5246#section-7.4.2>`_).
This is the common default when using a CA management tool.

.. NOTE::

   As of Go 1.15 (zrepl 0.3.0 and newer), the Go TLS / x509 library **requrires Subject Alternative Names**
   be present in certificates. You might need to re-generate your certificates using one of the :ref:`two alternatives
   provided below<transport-tcp+tlsclientauth-certgen>`.

   Note further that zrepl continues to use the CommonName field to assign client identities.
   Hence, we recommend to keep the Subject Alternative Name and the CommonName in sync.


Serve
~~~~~

::

    jobs:
      - type: sink
        root_fs: "pool2/backup_laptops"
        serve:
          type: tls
          listen: ":8888"
          listen_freebind: true # optional, default false
          ca:   /etc/zrepl/ca.crt
          cert: /etc/zrepl/prod.fullchain
          key:  /etc/zrepl/prod.key
          client_cns:
            - "laptop1"
            - "homeserver"

The ``ca`` field specified the certificate authority used to validate client certificates.
The ``client_cns`` list specifies a list of accepted client common names (which are also the client identities for this transport).
The ``listen_freebind`` field is :ref:`explained here <listen-freebind-explanation>`.

Connect
~~~~~~~

::

    jobs:
    - type: pull
      connect:
        type: tls
        address: "server1.foo.bar:8888"
        ca:   /etc/zrepl/ca.crt
        cert: /etc/zrepl/backupserver.fullchain
        key:  /etc/zrepl/backupserver.key
        server_cn: "server1"
        dial_timeout: # optional, default 10s

The ``ca`` field specifies the CA which signed the server's certificate (``serve.cert``).
The ``server_cn`` specifies the expected common name (CN) of the server's certificate.
It overrides the hostname specified in ``address``.
The connection fails if either do not match.

.. _transport-tcp+tlsclientauth-certgen:

.. _transport-tcp+tlsclientauth-2machineopenssl:

Mutual-TLS between Two Machines
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

However, for a two-machine setup, self-signed certificates distributed using an out-of-band mechanism will also work just fine:

Suppose you have a push-mode setup, with `backups.example.com` running the :ref:`sink job <job-sink>`, and `prod.example.com` running the :ref:`push job <job-push>`.
Run the following OpenSSL commands on each host, substituting HOSTNAME in both filenames and the interactive input prompt by OpenSSL:

.. code-block:: bash

   (name=HOSTNAME; openssl req -x509 -sha256 -nodes \
    -newkey rsa:4096 \
    -days 365 \
    -keyout $name.key \
    -out $name.crt -addext "subjectAltName = DNS:$name" -subj "/CN=$name")

Now copy each machine's ``HOSTNAME.crt`` to the other machine's ``/etc/zrepl/HOSTNAME.crt``, for example using `scp`.
The serve & connect configuration will thus look like the following:

::

   # on backups.example.com
   - type: sink
     serve:
       type: tls
       listen: ":8888"
       ca: "/etc/zrepl/prod.example.com.crt"
       cert: "/etc/zrepl/backups.example.com.crt"
       key: "/etc/zrepl/backups.example.com.key"
       client_cns:
         - "prod.example.com"
     ...

   # on prod.example.com
   - type: push
     connect:
       type: tls
       address:"backups.example.com:8888"
       ca: /etc/zrepl/backups.example.com.crt
       cert: /etc/zrepl/prod.example.com.crt
       key:  /etc/zrepl/prod.example.com.key
       server_cn: "backups.example.com"
     ...


Certificate Authority using EasyRSA
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

For more than two machines, it might make sense to set up a CA infrastructure.
Tools like `EasyRSA <https://github.com/OpenVPN/easy-rsa>`_ make this very easy:

::

     #!/usr/bin/env bash
     set -euo pipefail

     HOSTS=(backupserver prod1 prod2 prod3)

     curl -L https://github.com/OpenVPN/easy-rsa/releases/download/v3.0.7/EasyRSA-3.0.7.tgz > EasyRSA-3.0.7.tgz
     echo "157d2e8c115c3ad070c1b2641a4c9191e06a32a8e50971847a718251eeb510a8  EasyRSA-3.0.7.tgz" | sha256sum -c
     rm -rf EasyRSA-3.0.7
     tar -xf EasyRSA-3.0.7.tgz
     cd EasyRSA-3.0.7
     ./easyrsa
     ./easyrsa init-pki
     ./easyrsa build-ca nopass

     for host in "${HOSTS[@]}"; do
         ./easyrsa --subject-alt-name=DNS:$host build-serverClient-full $host nopass
         echo cert for host $host available at pki/issued/$host.crt
         echo key for host $host available at pki/private/$host.key
     done
     echo ca cert available at pki/ca.crt


.. _transport-ssh+stdinserver:

``ssh+stdinserver`` Transport
-----------------------------

``ssh+stdinserver`` uses the ``ssh`` command and some features of the server-side SSH ``authorized_keys`` file.
It is less efficient than other transports because the data passes through two more pipes.
However, it is fairly convenient to set up and allows the zrepl daemon to not be directly exposed to the internet, because all traffic passes through the system's SSH server.

The concept is inspired by `git shell <https://git-scm.com/docs/git-shell>`_ and `Borg Backup <https://borgbackup.readthedocs.io/en/stable/deployment.html>`_.
The implementation is provided by the Go package ``github.com/problame/go-netssh``.

.. NOTE::

   ``ssh+stdinserver`` generally provides inferior error detection and handling compared to the ``tcp`` and ``tls`` transports.
   When encountering such problems, consider using  ``tcp`` or ``tls`` transports, or help improve package go-netssh.

.. _transport-ssh+stdinserver-serve:

Serve
~~~~~

::

    jobs:
    - type: source
      serve:
        type: stdinserver
        client_identities:
        - "client1"
        - "client2"
      ...

First of all, note that ``type=stdinserver`` in this case:
Currently, only ``connect.type=ssh+stdinserver`` can connect to a ``serve.type=stdinserver``, but we want to keep that option open for future extensions.

The serving job opens a UNIX socket named after ``client_identity`` in the runtime directory.
In our example above, that is ``/var/run/zrepl/stdinserver/client1`` and ``/var/run/zrepl/stdinserver/client2``.

On the same machine, the ``zrepl stdinserver $client_identity`` command connects to ``/var/run/zrepl/stdinserver/$client_identity``.
It then passes its stdin and stdout file descriptors to the zrepl daemon via *cmsg(3)*.
zrepl daemon in turn combines them into an object implementing ``net.Conn``:
a ``Write()`` turns into a write to stdout, a ``Read()`` turns into a read from stdin.

Interactive use of the ``stdinserver`` subcommand does not make much sense.
However, we can force its execution when a user with a particular SSH pubkey connects via SSH.
This can be achieved with an entry in the ``authorized_keys`` file of the serving zrepl daemon.

::

    # for OpenSSH >= 7.2
    command="zrepl stdinserver CLIENT_IDENTITY",restrict CLIENT_SSH_KEY
    # for older OpenSSH versions
    command="zrepl stdinserver CLIENT_IDENTITY",no-port-forwarding,no-X11-forwarding,no-pty,no-agent-forwarding,no-user-rc CLIENT_SSH_KEY

* CLIENT_IDENTITY is substituted with an entry from ``client_identities`` in our example
* CLIENT_SSH_KEY is substituted with the public part of the SSH keypair specified in the ``connect.identity_file`` directive on the connecting host.

.. NOTE::

    You may need to adjust the ``PermitRootLogin`` option in ``/etc/ssh/sshd_config`` to ``forced-commands-only`` or higher for this to work.
    Refer to sshd_config(5) for details.

To recap, this is of how client authentication works with the ``ssh+stdinserver`` transport:

* Connections to the ``/var/run/zrepl/stdinserver/${client_identity}`` UNIX socket are blindly trusted by zrepl daemon.
  The connection client identity is the name of the socket, i.e. ``${client_identity}``.
* Thus, the runtime directory must be private to the zrepl user (this is checked by zrepl daemon)
* The admin of the host with the serving zrepl daemon controls the ``authorized_keys`` file.
* Thus, the administrator controls the mapping ``PUBKEY -> CLIENT_IDENTITY``.

.. _transport-ssh+stdinserver-connect:

Connect
~~~~~~~

::

    jobs:
    - type: pull
      connect:
        type: ssh+stdinserver
        host: prod.example.com
        user: root
        port: 22
        identity_file: /etc/zrepl/ssh/identity
        # options: # optional, default [], `-o` arguments passed to ssh
        # - "Compression=yes"
        # dial_timeout: 10s # optional, default 10s, max time.Duration until initial handshake is completed

The connecting zrepl daemon

#. Creates a pipe
#. Forks
#. In the forked process

   #. Replaces forked stdin and stdout with the corresponding pipe ends
   #. Executes the ``ssh`` binary found in ``$PATH``.

      #. The identity file (``-i``) is set to ``$identity_file``.
      #. The remote user, host and port correspond to those configured.
      #. Further options can be specified using the ``options`` field, which appends each entry in the list to the command line using ``-o $entry``.

#. Wraps the pipe ends in a ``net.Conn`` and returns it to the RPC layer.

As discussed in the section above, the connecting zrepl daemon expects that ``zrepl stdinserver $client_identity`` is  executed automatically via an ``authorized_keys`` file entry.

The ``known_hosts`` file used by the ssh command must contain an entry for ``connect.host`` prior to starting zrepl.
Thus, run the following on the pulling host's command line (substituting ``connect.host``):

::

    ssh -i /etc/zrepl/ssh/identity root@prod.example.com

.. NOTE::

    The environment variables of the underlying SSH process are cleared. ``$SSH_AUTH_SOCK`` will not be available.
    It is suggested to create a separate, unencrypted SSH key solely for that purpose.


.. _transport-local:

``local`` Transport
-------------------

The local transport can be used to implement :ref:`local replication <replication-local>`, i.e., push replication between a push and sink job defined in the same configuration file.

The ``listener_name`` is analogous to a hostname and must match between ``serve`` and ``connect``.
The ``client_identity`` is used by the sink as documented above.

::

    jobs:
    - type: sink
      serve:
        type: local
        listener_name: localsink
      ...

    - type: push
      connect:
        type: local
        listener_name: localsink
        client_identity: local_backup
        dial_timeout: 2s # optional, 0 for no timeout
      ...

