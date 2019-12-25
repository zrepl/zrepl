.. _binary releases: https://github.com/zrepl/zrepl/releases

.. _installation:

Installation
============

.. TIP::

    Note: check out the :ref:`tutorial` if you want a first impression of zrepl.

User Privileges
---------------

It is possible to run zrepl as an unprivileged user in combination with
`ZFS delegation <https://www.freebsd.org/doc/handbook/zfs-zfs-allow.html>`_.
Also, there is the possibility to run it in a jail on FreeBSD by delegating a dataset to the jail.
However, until we get around documenting those setups, you will have to run zrepl as root or experiment yourself :)

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
    * - FreeBSD
      - ``pkg install zrepl``
      - `<https://www.freshports.org/sysutils/zrepl/>`_
    * - MacOS
      - ``brew install zrepl``
      - Available on `homebrew <https://brew.sh>`_
    * - Arch Linux
      - ``yay install zrepl``
      - Available on `AUR <https://aur.archlinux.org/packages/zrepl>`_
    * - Fedora
      - ``dnf install zrepl``
      - Available on `COPR <https://copr.fedorainfracloud.org/coprs/poettlerric/zrepl/>`_
    * - CentOS/RHEL
      - ``yum install zrepl``
      - Available on `COPR <https://copr.fedorainfracloud.org/coprs/poettlerric/zrepl/>`_
    * - Debian + Ubuntu
      - ``apt install zrepl``
      - APT repository config :ref:`see below <installation-apt-repos>`
    * - OmniOS
      - ``pkg install zrepl``
      - Available since `r151030 <https://pkg.omniosce.org/r151030/extra/en/search.shtml?token=zrepl&action=Search>`_
    * - Void Linux
      - ``xbps-install zrepl``
      - Available since `a88a2a4 <https://github.com/void-linux/void-packages/commit/a88a2a4d7bf56072dadf61ab56b8424e39155890>`_
    * - Others
      -
      - Use `binary releases`_ or build from source.

.. _installation-apt-repos:

Debian / Ubuntu APT repositories
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

We maintain APT repositories for Debian, Ubuntu and derivatives.
The fingerprint of the signing key is ``E101 418F D3D6 FBCB 9D65  A62D 7086 99FC 5F2E BF16``.
It is available at `<https://zrepl.cschwarz.com/apt/apt-key.asc>`_ .
Please open an issue `in the packaging repository <https://github.com/zrepl/debian-binary-packaging>`_ if you encounter any issues with the repository.

The following snippet configure the repository for your Debian or Ubuntu release:

::

    apt update && apt install curl gnupg lsb-release; \
    ARCH="$(dpkg --print-architecture)"; \
    CODENAME="$(lsb_release -i -s | tr '[:upper:]' '[:lower:]') $(lsb_release -c -s | tr '[:upper:]' '[:lower:]')"; \
    echo "Using Distro and Codename: $CODENAME"; \
    (curl https://zrepl.cschwarz.com/apt/apt-key.asc | apt-key add -) && \
    (echo "deb [arch=$ARCH] https://zrepl.cschwarz.com/apt/$CODENAME main" > /etc/apt/sources.list.d/zrepl.list) && \
    apt update


.. NOTE::

   Until zrepl reaches 1.0, all APT repositories will be updated to the latest zrepl release immediately.
   This includes breaking changes between zrepl versions.
   Use ``apt-mark hold zrepl`` to prevent upgrades of zrepl.

Compile From Source
~~~~~~~~~~~~~~~~~~~

Producing a release requires **Go 1.11** or newer and **Python 3** + **pip3** + ``docs/requirements.txt`` for the Sphinx documentation.
A tutorial to install Go is available over at `golang.org <https://golang.org/doc/install>`_.
Python and pip3 should probably be installed via your distro's package manager.

Alternatively, you can use the Docker build process:
it is used to produce the official zrepl `binary releases`_
and serves as a reference for build dependencies and procedure:

::

    git clone https://github.com/zrepl/zrepl.git
    cd zrepl
    sudo docker build -t zrepl_build -f build.Dockerfile .
    sudo docker run -it --rm \
        -v "${PWD}:/src" \
        --user "$(id -u):$(id -g)" \
        zrepl_build make release

Alternatively, you can install build dependencies on your local system and then build in your ``$GOPATH``:

::

    mkdir -p "${GOPATH}/src/github.com/zrepl/zrepl"
    git clone https://github.com/zrepl/zrepl.git "${GOPATH}/src/github.com/zrepl/zrepl"
    cd "${GOPATH}/src/github.com/zrepl/zrepl"
    python3 -m venv3
    source venv3/bin/activate
    ./lazy.sh devsetup
    make release

The Python venv is used for the documentation build dependencies.
If you just want to build the zrepl binary, leave it out and use `./lazy.sh godep` instead.
Either way, all build results are located in the ``artifacts/`` directory.

.. NOTE::

    It is your job to install the apropriate binary in the zrepl users's ``$PATH``, e.g. ``/usr/local/bin/zrepl``.
    Otherwise, the examples in the :ref:`tutorial` may need to be adjusted.

What next?
----------

Read the :ref:`configuration chapter<configuration_toc>` and then continue with the :ref:`usage chapter<usage>`.

**Reminder**: If you want a quick introduction, please read the :ref:`tutorial`.
