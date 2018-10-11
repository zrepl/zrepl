.. |break_config| replace:: **[CONFIG]**
.. |break| replace:: **[BREAK]**
.. |bugfix| replace:: [BUG]
.. |docs| replace:: [DOCS]
.. |feature| replace:: [FEATURE]

.. _changelog:

Changelog
=========

The changelog summarizes bugfixes that are deemed relevant for users and package maintainers.
Developers should consult the git commit log or GitHub issue tracker.

e use the following annotations for classifying changes:

* |break_config| Change that breaks the config.
  As a package maintainer, make sure to warn your users about config breakage somehow.
* |break| Change that breaks interoperability or persistent state representation with previous releases.
  As a package maintainer, make sure to warn your users about config breakage somehow.
  Note that even updating the package on both sides might not be sufficient, e.g. if persistent state needs to be migrated to a new format.
* |feature| Change that introduces new functionality.
* |bugfix| Change that fixes a bug, no regressions or incompatibilities expected.
* |docs| Change to the documentation.

0.1 (unreleased)
------------------

This release is a milestone for zrepl and required significant refactoring if not rewrites of substantial parts of the application.
It breaks both configuration and transport format, and thus requires manual intervention and updates on both sides of a replication setup.

Notes to Package Maintainer
~~~~~~~~~~~~~~~~~~~~~~~~~~~

* If the daemon crashes, the stack trace produced by the Go runtime and possibly diagnostic output of zrepl will be written to stderr.
  This behavior is independent from the ``stdout`` outlet type.
  Please make sure the stderr output of the daemon is captured to a file.
  Rotation should not be necessary because stderr is not written to under normal circumstances.
  To conserve precious stack traces, make sure that multiple service restarts do not directly discard previous stderr output.
* Make it obvious for users how to set the ``GOTRACEBACK`` environment variable to ``GOTRACEBACK=crash``.
  This functionality will cause SIGABRT on panics and can be used to capture a coredump of the panicking process.
  To that extend, make sure that your package build system, your OS's coredump collection and the Go delve debugger work together.
  Use your build system to package the Go program in `this tutorial on Go coredumps and the delve debugger <https://rakyll.org/coredumps/>`_ , and make sure the symbol resolution etc. work on coredumps captured from the binary produced by your build system. (Special focus on symbol stripping, etc.)

Changes
~~~~~~~

* |feature| :issue:`55` : Push replication (see :ref:`push job <job-push>` and :ref:`sink job <job-sink>`)
* |feature| :ref:`TCP Transport <transport-tcp>`
* |feature| :ref:`TCP + TLS client authentication transport <transport-tcp+tlsclientauth>`
* |feature| :issue:`78` TODO MERGE COMMIT Replication protocol rewrite

  * Uses ``github.com/problame/go-streamrpc`` for RPC layer
  * |break| zrepl 0.1 and restart on both sides of a replication setup is required
  * |feature| :issue:`83`:  Improved error handling of network-level errors (zrepl retries instead of failing the entire job)
  * |bugfix| :issue:`75` :issue:`81`: use connection timeouts and protocol-level heartbeats
  * |break| |break_config|: mappings are no longer supported

    * Receiving sides (``pull`` and ``sink`` job) specify a single ``root_fs``.
      Received filesystems are then stored *per client* in ``${root_fs}/${client_identity}``.

* |feature| |break| |break_config| Manual snapshotting + triggering of replication

  * |feature| :issue:`69`: include manually created snapshots in replication
  * |break_config| ``manual`` and ``periodic`` :ref:`snapshotting types <job-snapshotting-spec>`
  * |feature| ``zrepl wakeup JOB`` subcommand to trigger *just* replication

* |feature| |break| |break_config| New pruning system

  * The active side of a replication (pull or push) decides what to prune for both sender and receiver.
    The RPC protocol is used to execute the destroy operations on the remote side.
  * New pruning policies (see :ref:`configuration documentation <prune>` )

    * The decision what snapshots shall be pruned is now made based on *keep rules*
    * |feature| :issue:`68`: keep rule ``not_replicated`` prevents divergence of sender and receiver

  * |feature| |break| Bookmark pruning is no longer necessary

    * Per filesystem, zrepl creates a single bookmark (``#zrepl_replication_cursor``) and moves it forward with the most recent successfully replicated snapshot on the receiving side.
    * Old bookmarks created prior to zrepl 0.1 (named like their corresponding snapshot) must be deleted manually.
    * |break_config| ``keep_bookmarks`` parameter of the ``grid`` keep rule has been removed

* |feature| ``zrepl status`` for live-updating replication progress (it's really cool!)
* |feature| :issue:`67`: Expose `Prometheus <https://prometheus.io>`_ metrics via HTTP (:ref:`config docs <monitoring-prometheus>`)

* |break_config| Logging outlet types must be specified using the ``type`` instead of ``outlet`` key
* |break| :issue:`53`: CLI: ``zrepl control *`` subcommands have been made direct subcommands of ``zrepl *``

* |bugfix| :issue:`81` :issue:`77` : handle failed accepts correctly (``source`` job)

.. |lastrelease| replace:: 0.0.3

Previous Releases
-----------------

.. NOTE::
    Due to limitations in our documentation system, we only show the changelog since the last release and the time this documentation is built.
    For the changelog of previous releases, use the version selection in the hosted version of these docs at `zrepl.github.io <https://zrepl.github.io>`_.
    
    


W
