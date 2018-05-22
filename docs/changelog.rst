.. |break_config| replace:: **[BREAK]**
.. |break| replace:: **[BREAK]**
.. |bugfix| replace:: [BUG]
.. |docs| replace:: [DOCS]
.. |feature| replace:: [FEATURE]

Changelog
=========

The changelog summarized bugfixes that are deemed relevant for users.
Developers should consult the git commit log or GitHub issue tracker.

0.0.4 (unreleased)
------------------

* |feature| :issue:`67`: Expose `Prometheus <https://prometheus.io>`_ metrics via HTTP (:ref:`config docs <monitoring-prometheus>`)
* |bugfix| Goroutine leak on ssh transport connection timeouts

0.0.3
-----

* |break_config| |feature| :issue:`34`: automatic bookmarking of snapshots

  * Snapshots are automatically bookmarked and pruning of bookmarks **must** be configured.
  * This breaks existing configuration: ``grid`` :ref:`prune policy <prune-retention-grid>`  specifications require the new ``keep_bookmarks`` parameter.
  * Make sure to understand the meaning bookmarks have for :ref:`maximum replication downtime <replication-downtime>`.
  * Example: :sampleconf:`pullbackup/productionhost.yml`

* |break| :commit:`ccd062e`: ``ssh+stdinserver`` transport: changed protocol requires daemon restart on both sides

  * The delicate procedure of talking to the serving-side zrepl daemon via the stdinserver proxy command now has better error handling.
  * This includes handshakes between client+proxy and client + remote daemo, which is not implemented in previous versions of zrepl.
  * The connecting side will therefore time out, with the message ``dial_timeout of 10s exceeded``.
  * Both sides of a replication setup must be updated and restarted. Otherwise the connecting side will hang and not time out.

* |break_config| :commit:`2bfcfa5`: first outlet in ``global.logging`` is now used for logging meta-errors, for example problems encountered when writing to other outlets.
* |feature| :issue:`10`: ``zrepl control status`` subcommand

  * Allows inspection of job activity per task and their log output at runtime.
  * Supports ``--format raw`` option for JSON output, usable for monitoring from scripts.

* |feature| :commit:`d7f3fb9`: subcommand bash completions

  * Package maintainers should install this as appropriate.

* |bugfix| :issue:`61`: fix excessive memory usage
* |bugfix| :issue:`8` and :issue:`56`: ``ssh+stdinserver`` transport properly reaps SSH child processes
* |bugfix| :commit:`cef63ac`: ``human`` format now prints non-string values correctly
* |bugfix| :issue:`26`: slow TCP outlets no longer block the daemon
* |docs| :issue:`64`: tutorial: document ``known_host`` file entry

0.0.2
-----

* |break_config| :commit:`b95260f`: ``global.logging`` is no longer a dictionary but a list

* |break_config| :commit:`3e647c1`: ``source`` job field ``datasets`` renamed to ``filesystems``

  * **NOTE**: zrepl will parse missing ``filesystems`` field as an empty filter,
    i.e. no filesystems are presented to the other side.

* |bugfix| :commit:`72d2885` fix aliasing bug with root `<` subtree wildcard

  * Filesystems paths with final match at blank `s` subtree wildcard are now appended to the target path
  * Non-root subtree wildcards, e.g. `zroot/foo/bar<` still map directrly onto the target path

* Support days (``d``) and weeks (``w``) in durations

* Docs

  * Ditch Hugo, move to Python Sphinx
  * Improve & simplify tutorial (single SSH key per installation)
  * Document pruning policies
  * Document job types
  * Document logging
  * Start updating implementation overview


0.0.1
-----

* Initial release
