.. |break_config| replace:: **[CONFIG]**
.. |break| replace:: **[BREAK]**
.. |bugfix| replace:: [BUG]
.. |docs| replace:: [DOCS]
.. |feature| replace:: [FEATURE]
.. |mig| replace:: **[MIGRATION]**

.. _changelog:

Changelog
=========

The changelog summarizes bugfixes that are deemed relevant for users and package maintainers.
Developers should consult the git commit log or GitHub issue tracker.

We use the following annotations for classifying changes:

* |break_config| Change that breaks the config.
  As a package maintainer, make sure to warn your users about config breakage somehow.
* |break| Change that breaks interoperability or persistent state representation with previous releases.
  As a package maintainer, make sure to warn your users about config breakage somehow.
  Note that even updating the package on both sides might not be sufficient, e.g. if persistent state needs to be migrated to a new format.
* |mig| Migration that must be run by the user.
* |feature| Change that introduces new functionality.
* |bugfix| Change that fixes a bug, no regressions or incompatibilities expected.
* |docs| Change to the documentation.

0.1.2 (TBD)
-----------

* Linux ARM64 Docker build support & binary builds
* Go modules for dependency management both inside and outside of GOPATH
  (``lazy.sh`` and ``Makefile`` force ``GO111MODULE=on``)
* |feature| Use ``zfs destroy pool/fs@snap1,snap2,...`` CLI feature if available
* |feature| :ref:`Pre- and Post-Snapshot Hooks <job-snapshotting-hooks>`
  with built-in support for MySQL and Postgres checkpointing
  as well as custom scripts

0.1.1
-----

* |bugfix| :issue:`162` :commit:`d6304f4` : fix I/O timeout errors on variable receive rate

  * A significant reduction or sudden stall of the receive rate (e.g. recv pool has other I/O to do)
    would cause a ``writev I/O timeout`` error after approximately ten seconds.

0.1
---

This release is a milestone for zrepl and required significant refactoring if not rewrites of substantial parts of the application.
It breaks both configuration and transport format, and thus requires manual intervention and updates on both sides of a replication setup.

.. DANGER::
   The changes in the pruning system for this release require you to explicitly define **keep rules**:
   for any snapshot that you want to keep, at least one rule must match.
   This is different from previous releases where pruning only affected snapshots with the configured snapshotting prefix.
   Make sure that snapshots to be kept or ignored by zrepl are covered, e.g. by using the ``regex`` keep rule.
   :ref:`Learn more in the config docs... <prune>`


Notes to Package Maintainers
~~~~~~~~~~~~~~~~~~~~~~~~~~~~

* Notify users about config changes and migrations (see changes attributed with |break| and |mig| below)
* If the daemon crashes, the stack trace produced by the Go runtime and possibly diagnostic output of zrepl will be written to stderr.
  This behavior is independent from the ``stdout`` outlet type.
  Please make sure the stderr output of the daemon is captured somewhere.
  To conserve precious stack traces, make sure that multiple service restarts do not directly discard previous stderr output.
* Make it obvious for users how to set the ``GOTRACEBACK`` environment variable to ``GOTRACEBACK=crash``.
  This functionality will cause SIGABRT on panics and can be used to capture a coredump of the panicking process.
  To that extend, make sure that your package build system, your OS's coredump collection and the Go delve debugger work together.
  Use your build system to package the Go program in `this tutorial on Go coredumps and the delve debugger <https://rakyll.org/coredumps/>`_ , and make sure the symbol resolution etc. work on coredumps captured from the binary produced by your build system. (Special focus on symbol stripping, etc.)
* Consider using the ``zrepl configcheck`` subcommand in startup scripts to abort a restart that would fail due to an invalid config.

Changes
~~~~~~~

* |break| |mig| Placeholder property representation changed

  * The :ref:`placeholder property <replication-placeholder-property>` now uses ``on|off`` as values
    instead of hashes of the dataset path. This permits renames of the sink filesystem without
    updating all placeholder properties.
  * Relevant for 0.0.X-0.1-rc* to 0.1 migrations
  * Make sure your config is valid with ``zrepl configcheck``
  * Run ``zrepl migrate 0.0.X:0.1:placeholder``

* |feature| :issue:`55` : Push replication (see :ref:`push job <job-push>` and :ref:`sink job <job-sink>`)
* |feature| :ref:`TCP Transport <transport-tcp>`
* |feature| :ref:`TCP + TLS client authentication transport <transport-tcp+tlsclientauth>`
* |feature| :issue:`111`: RPC protocol rewrite

  * |break| Protocol breakage; Update and restart of all zrepl daemons is required.
  * Use `gRPC <https://grpc.io/>`_ for control RPCs and a custom protocol for bulk data transfer.
  * Automatic retries for network-temporary errors

    * Limited to errors during replication for this release.
      Addresses the common problem of ISP-forced reconnection at night, but will become
      way more useful with resumable send & recv support.
      Pruning errors are handled per FS, i.e., a prune RPC is attempted at least once per FS.

* |feature| Proper timeout handling for the :ref:`SSH transport <transport-ssh+stdinserver>`

  * |break| Requires Go 1.11 or later.
  
* |break| |break_config|: mappings are no longer supported

  * Receiving sides (``pull`` and ``sink`` job) specify a single ``root_fs``.
    Received filesystems are then stored *per client* in ``${root_fs}/${client_identity}``.
    See :ref:`job-overview` for details.

* |feature| |break| |break_config| Manual snapshotting + triggering of replication

  * |feature| :issue:`69`: include manually created snapshots in replication
  * |break_config| ``manual`` and ``periodic`` :ref:`snapshotting types <job-snapshotting-spec>`
  * |feature| ``zrepl signal wakeup JOB`` subcommand to trigger replication + pruning
  * |feature| ``zrepl signal reset JOB`` subcommand to abort current replication + pruning

* |feature| |break| |break_config| New pruning system

  * The active side of a replication (pull or push) decides what to prune for both sender and receiver.
    The RPC protocol is used to execute the destroy operations on the remote side.
  * New pruning policies (see :ref:`configuration documentation <prune>` )

    * The decision what snapshots shall be pruned is now made based on *keep rules*
    * |feature| :issue:`68`: keep rule ``not_replicated`` prevents divergence of sender and receiver

  * |feature| |break| Bookmark pruning is no longer necessary

    * Per filesystem, zrepl creates a single bookmark (``#zrepl_replication_cursor``) and moves it forward with the most recent successfully replicated snapshot on the receiving side.
    * Old bookmarks created by prior versions of zrepl (named like their corresponding snapshot) must be deleted manually.
    * |break_config| ``keep_bookmarks`` parameter of the ``grid`` keep rule has been removed

* |feature| ``zrepl status`` for live-updating replication progress (it's really cool!)
* |feature| :ref:`Snapshot- & pruning-only job type <job-snap>` (for local snapshot management)
* |feature| :issue:`67`: Expose `Prometheus <https://prometheus.io>`_ metrics via HTTP (:ref:`config docs <monitoring-prometheus>`)

  * Compatible Grafana dashboard shipping in ``dist/grafana``

* |break_config| Logging outlet types must be specified using the ``type`` instead of ``outlet`` key
* |break| :issue:`53`: CLI: ``zrepl control *`` subcommands have been made direct subcommands of ``zrepl *``
* |bugfix| Goroutine leak on ssh transport connection timeouts
* |bugfix| :issue:`81` :issue:`77` : handle failed accepts correctly (``source`` job)
* |bugfix| :issue:`100`: fix incompatibility with ZoL 0.8
* |feature| :issue:`115`: logging: configurable syslog facility
* |feature| Systemd unit file in ``dist/systemd``

.. |lastrelease| replace:: 0.0.3

Previous Releases
-----------------

.. NOTE::
    Due to limitations in our documentation system, we only show the changelog since the last release and the time this documentation is built.
    For the changelog of previous releases, use the version selection in the hosted version of these docs at `zrepl.github.io <https://zrepl.github.io>`_.
