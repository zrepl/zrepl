.. include:: global.rst.inc

.. |break_config| replace:: **[CONFIG]**
.. |break| replace:: **[BREAK]**
.. |bugfix| replace:: [BUG]
.. |docs| replace:: [DOCS]
.. |feature| replace:: [FEATURE]
.. |mig| replace:: **[MIGRATION]**
.. |maint| replace:: [MAINT]

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
* |maint| Maintenance changes.

0.4.0
-----

* |break| Change syntax to trigger a job replication, rename ``zrepl signal wakeup JOB`` to ``zrepl signal replication JOB``
* |feature| support setting zfs send / recv flags in the config (send: ``-wLcepbS`` , recv: ``-ox`` ).
  Config docs :ref:`here <job-send-options>` and :ref:`here <job-recv-options>` .
* |feature| parallel replication is now configurable (disabled by default, :ref:`config docs here <replication-option-concurrency>` ).
* |feature| New ``zrepl status`` UI:

  * Interactive job selection.
  * Interactively ``zrepl signal`` jobs.
  * Filter filesystems in the job view by name.
  * An approximation of the old UI is still included as `--mode legacy` but will be removed in a future release of zrepl.

* |bugfix| Actually use concurrency when listing zrepl abstractions & doing size estimation.
  These operations were accidentally made sequential in zrepl 0.3.
* |bugfix| Job hang-up during second replication attempt.
* |bugfix| Data races conditions in the dataconn rpc stack.
* |maint| Update to protobuf v1.25 and grpc 1.35.

For users who skipped the 0.3.1 update: please make sure your pruning grid config is correct.
The following bugfix in 0.3.1 :issue:`caused problems for some users <400>`:

* |bugfix| pruning: ``grid``:  add all snapshots that do not match the regex to the rule's destroy list.

.. NOTE::
  |  zrepl is a spare-time project primarily developed by `Christian Schwarz <https://cschwarz.com>`_.
  |  You can support maintenance and feature development through one of the following services:
  |  |Donate via Patreon| |Donate via GitHub Sponsors| |Donate via Liberapay| |Donate via PayPal|
  |  Note that PayPal processing fees are relatively high for small donations.
  |  For SEPA wire transfer and **commercial support**, please `contact Christian directly <https://cschwarz.com>`_.

0.3.1
-----

Mostly a bugfix release for :ref:`zrepl 0.3 <release-0.3>`.

* |feature| pruning: add optional ``regex`` field to ``last_n`` rule
* |docs| pruning: ``grid`` : improve documentation and add an example
* |bugfix| pruning: ``grid``:  add all snapshots that do not match the regex to the rule's destroy list.
  This brings the implementation in line with the docs.
* |bugfix| ``easyrsa`` script in docs
* |bugfix| platformtest: fix skipping encryption-only tests on systems that don't support encryption
* |bugfix| replication: report AttemptDone if no filesystems are replicated
* |feature| status + replication: warning if replication succeeeded without any filesystem being replicated
* |docs| update multi-job & multi-host setup section
* RPM Packaging
* CI infrastructure rework
* Continuous deployment of that new `stable` branch to zrepl.github.io.

.. _release-0.3:

0.3
---

This is a big one! Headlining features:

* **Resumable Send & Recv Support**
  No knobs required, automatically used where supported.
* **Encrypted Send & Recv Support** for OpenZFS native encryption,
  :ref:`configurable <job-send-options>` at the job level, i.e., for all filesystems a job is responsible for.
* **Replication Guarantees**
  Automatic use of ZFS holds and bookmarks to protect a replicated filesystem from losing synchronization between sender and receiver.
  By default, zrepl guarantees that incremental replication will always be possible and interrupted steps will always be resumable.

.. TIP::

   We highly recommend studying the updated :ref:`overview section of the configuration chapter <overview-how-replication-works>` to understand how replication works.

.. TIP::

   Go 1.15 changed the default TLS validation policy to **require Subject Alternative Names (SAN) in certificates**.
   The openssl commands we provided in the quick-start guides up to and including the zrepl 0.3 docs seem not to work properly.
   If you encounter certificate validation errors regarding SAN and wish to continue to use your old certificates, start the zrepl daemon with env var ``GODEBUG=x509ignoreCN=0``.
   Alternatively, generate new certificates with SANs (see :ref:`both options int the TLS transport docs <transport-tcp+tlsclientauth-certgen>` ).

Quick-start guides:

* We have added :ref:`another quick-start guide for a typical workstation use case for zrepl <quickstart-backup-to-external-disk>`.
  Check it out to learn how you can use zrepl to back up your workstation's OpenZFS natively-encrypted root filesystem to an external disk.

Additional changelog:

* |break| Go 1.15 TLS changes mentioned above.
* |break| |break_config| **more restrictive job names than in prior zrepl versions**
  Starting with this version, job names are going to be embedded into ZFS holds and bookmark names (see :ref:`this section for details <zrepl-zfs-abstractions>`).
  Therefore you might need to adjust your job names.
  **Note that jobs** cannot be renamed easily **once you start using zrepl 0.3.**
* |break| |mig| replication cursor representation changed

  * zrepl now manages the :ref:`replication cursor bookmark <zrepl-zfs-abstractions>` per job-filesystem tuple instead of a single replication cursor per filesystem.
    In the future, this will permit multiple sending jobs to send from the same filesystems.
  * ZFS does not allow bookmark renaming, thus we cannot migrate the old replication cursors.
  * zrepl 0.3 will automatically create cursors in the new format for new replications, and warn if it still finds ones in the old format.
  * Run ``zrepl migrate replication-cursor:v1-v2`` to safely destroy old-format cursors.
    The migration will ensure that only those old-format cursors are destroyed that have been superseeded by new-format cursors.

* |feature| New option ``listen_freebind`` (tcp, tls, prometheus listener)
* |feature| :issue:`341` Prometheus metric for failing replications + corresponding Grafana panel
* |feature| :issue:`265` transport/tcp: support for CIDR masks in client IP whitelist
* |feature| documented subcommand to generate ``bash`` and ``zsh`` completions
* |feature| :issue:`307` ``chrome://trace`` -compatible activity tracing of zrepl daemon activity
* |feature| logging: trace IDs for better log entry correlation with concurrent replication jobs
* |feature| experimental environment variable for parallel replication (see :issue:`306` )
* |bugfix| missing logger context vars in control connection handlers
* |bugfix| improved error messages on ``zfs send`` errors
* |bugfix| |docs| snapshotting: clarify sync-up behavior and warn about filesystems
* |bugfix| transport/ssh: do not leak zombie ssh process on connection failures
  that will not be snapshotted until the sync-up phase is over
* |docs| Installation: :ref:`FreeBSD jail with iocage <installation-freebsd-jail-with-iocage>`
* |docs| Document new replication features in the :ref:`config overview <overview-how-replication-works>` and :repomasterlink:`replication/design.md`.
* **[MAINTAINER NOTICE]** New platform tests in this version, please make sure you run them for your distro!
* **[MAINTAINER NOTICE]** Please add the shell completions to the zrepl packages.

0.2.1
-----

* |feature| Illumos (and Solaris) compatibility and binary builds (thanks, `MNX.io <https://mnx.io>`_ )
* |feature| 32bit binaries for Linux and FreeBSD (untested, though)
* |bugfix| better error messages in ``ssh+stdinserver`` transport
* |bugfix| systemd + ``ssh+stdinserver``: automatically create ``/var/run/zrepl/stdinserver``
* |bugfix| crash if Prometheus listening socket cannot be opened

* [MAINTAINER NOTICE] ``Makefile`` refactoring, see :commit:`080f2c0`

0.2
---

* |feature| :ref:`Pre- and Post-Snapshot Hooks <job-snapshotting-hooks>`
  with built-in support for MySQL and Postgres checkpointing
  as well as custom scripts (thanks, `@overhacked <https://github.com/overhacked>`_!)
* |feature| Use ``zfs destroy pool/fs@snap1,snap2,...`` CLI feature if available
* |feature| Linux ARM64 Docker build support & binary builds
* |feature| ``zrepl status`` now displays snapshotting reports
* |feature| ``zrepl status --job <JOBNAME>`` filter flag
* |bugfix| i386 build
* |bugfix| early validation of host:port tuples in config
* |bugfix| ``zrepl status`` now supports ``TERM=screen`` (tmux on FreeBSD / FreeNAS)
* |bugfix| ignore *connection reset by peer* errors when shutting down connections
* |bugfix| correct error messages when receive-side pool or ``root_fs`` dataset is not imported
* |bugfix| fail fast for misconfigured local transport
* |bugfix| race condition in replication report generation would crash the daemon when running ``zrepl status``
* |bugfix| rpc goroutine leak in ``push`` mode if zfs recv fails on the ``sink`` side
* [MAINTAINER NOTICE] Go modules for dependency management both inside and outside of GOPATH
  (``lazy.sh`` and ``Makefile`` force ``GO111MODULE=on``)
* [MAINTAINER NOTICE] ``make platformtest`` target to check zrepl's ZFS abstractions (screen scraping, etc.).
  These tests only work on a system with ZFS installed, and must be run as root because they create a file-backed pool for each test case.
  The pool name ``zreplplatformtest`` is reserved for this use case.
  Only run ``make platformtest`` on test systems, e.g. a FreeBSD VM image.

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
