.. |break_config| replace:: **[BREAK]**
.. |bugfix| replace:: [BUG]

Changelog
=========

The changelog summarized bugfixes that are deemed relevant for users.
Developers should consult the git commit log or GitHub issue tracker.

0.0.2
-----

Breaking
~~~~~~~~

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
