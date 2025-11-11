.. _installation-packages:

.. _binary releases: https://github.com/LyingCak3/zrepl/releases

Packages
--------

zrepl source releases are signed & tagged by the author in the git repository.
Your OS vendor may provide binary packages of zrepl through the package manager.
Additionally, `binary releases`_ are provided on GitHub.
The following list may be incomplete, feel free to submit a PR with an update:

.. list-table::
    :header-rows: 1

    * - OS / Distro
      - Install Command
      - Link
    * - any
      - Statically linked binaries.
      - `Official GitHub releases <binary releases_>`_
    * - FreeBSD
      - ``pkg install zrepl``
      - `<https://www.freshports.org/sysutils/zrepl/>`_

        :ref:`installation-freebsd-jail-with-iocage`
    * - FreeNAS
      -
      - :ref:`installation-freebsd-jail-with-iocage`
    * - MacOS
      - ``brew install zrepl``
      - Available on `homebrew <https://brew.sh>`_
    * - Arch Linux
      - ``yay install zrepl``
      - Available on `AUR <https://aur.archlinux.org/packages/zrepl>`_
    * - Fedora / RHEL / OpenSUSE
      - ``dnf install zrepl``
      - :ref:`RPM repository config <installation-rpm-repos>`
    * - Debian + Ubuntu
      - ``apt install zrepl``
      - :ref:`APT repository config <installation-apt-repos>`
    * - OmniOS
      - ``pkg install zrepl``
      - Available since `r151030 <https://pkg.omniosce.org/r151030/extra/en/search.shtml?token=zrepl&action=Search>`_
    * - Void Linux
      - ``xbps-install zrepl``
      - Available since `a88a2a4 <https://github.com/void-linux/void-packages/commit/a88a2a4d7bf56072dadf61ab56b8424e39155890>`_
    * - any
      - Build from source
      - :repomasterlink:`README.md`.
