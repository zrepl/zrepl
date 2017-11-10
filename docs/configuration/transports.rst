.. highlight:: bash

Transports
==========

A transport provides an authenticated `io.ReadWriteCloser <https://golang.org/pkg/io/#ReadWriteCloser>`_ to the RPC layer.
(An ``io.ReadWriteCloser`` is essentially a bidirectional reliable communication channel.)

Currently, only the ``ssh+stdinserver`` transport is supported.

.. _transport-ssh+stdinserver:

``ssh+stdinserver`` Transport
-----------------------------

The way the ``ssh+stdinserver`` transport works is inspired by `git shell <https://git-scm.com/docs/git-shell>`_ and `Borg Backup <https://borgbackup.readthedocs.io/en/stable/deployment.html>`_.
It is implemented in the Go package ``github.com/zrepl/zrepl/sshbytestream``.
The config excerpts are taken from the :ref:`tutorial` which you should complete before reading further.

.. _transport-ssh+stdinserver-serve:

Serve Mode
~~~~~~~~~~

::

    jobs:
    - name: pull_backup
      type: source
      serve:
        type: stdinserver
        client_identity: backup-srv.example.com
      ...

The serving job opens a UNIX socket named after ``client_identity`` in the runtime directory, e.g. ``/var/run/zrepl/stdinserver/backup-srv.example.com``.

On the same machine, the ``zrepl stdinserver $client_identity`` command connects to that socket.
For example, ``zrepl stdinserver backup-srv.example.com`` connects to the UNIX socket ``/var/run/zrepl/stdinserver/backup-srv.example.com``.

It then passes its stdin and stdout file descriptors to the zrepl daemon via *cmsg(3)*.
zrepl daemon in turn combines them into an ``io.ReadWriteCloser``:
a ``Write()`` turns into a write to stdout, a ``Read()`` turns into a read from stdin.

Interactive use of the ``stdinserver`` subcommand does not make much sense.
However, we can force its execution when a user with a particular SSH pubkey connects via SSH.
This can be achieved with an entry in the ``authorized_keys`` file of the serving zrepl daemon.

::

    # for OpenSSH >= 7.2
    command="zrepl stdinserver CLIENT_IDENTITY",restrict CLIENT_SSH_KEY
    # for older OpenSSH versions
    command="zrepl stdinserver CLIENT_IDENTITY",no-port-forwarding,no-X11-forwarding,no-pty,no-agent-forwarding,no-user-rc CLIENT_SSH_KEY

* CLIENT_IDENTITY is substituted with ``backup-srv.example.com`` in our example
* CLIENT_SSH_KEY is substituted with the public part of the SSH keypair specified in the ``connect`` directive on the connecting host.

.. NOTE::

    You may need to adjust the ``PermitRootLogin`` option in ``/etc/ssh/sshd_config`` to ``forced-commands-only`` or higher for this to work.
    Refer to sshd_config(5) for details.

To recap, this is of how client authentication works with the ``ssh+stdinserver`` transport:

* Connections to the ``client_identity`` UNIX socket are blindly trusted by zrepl daemon.
* Thus, the runtime directory must be private to the zrepl user (checked by zrepl daemon)
* The admin of the host with the serving zrepl daemon controls the ``authorized_keys`` file.
* Thus, the administrator controls the mapping ``PUBKEY -> CLIENT_IDENTITY``.

.. _transport-ssh+stdinserver-connect:

Connect Mode
~~~~~~~~~~~~

::

    jobs:
    - name: pull_app-srv
      type: pull
      connect:
        type: ssh+stdinserver
        host: app-srv.example.com
        user: root
        port: 22
        identity_file: /etc/zrepl/ssh/identity
        options: # optional
        - "Compression=on"

The connecting zrepl daemon

#. Creates a pipe
#. Forks
#. In the forked process

   #. Replaces forked stdin and stdout with the corresponding pipe ends
   #. Executes the ``ssh`` binary found in ``$PATH``.

      #. The identity file (``-i``) is set to ``$identity_file``.
      #. The remote user, host and port correspond to those configured.
      #. Further options can be specified using the ``options`` field, which appends each entry in the list to the command line using ``-o $entry``.

1. Wraps the pipe ends in an ``io.ReadWriteCloser`` and uses it for RPC.

As discussed in the section above, the connecting zrepl daemon expects that ``zrepl stdinserver $client_identity`` is  executed automatically via an ``authorized_keys`` file entry.

.. NOTE::

    The environment variables of the underlying SSH process are cleared. ``$SSH_AUTH_SOCK`` will not be available.
    It is suggested to create a separate, unencrypted SSH key solely for that purpose.

