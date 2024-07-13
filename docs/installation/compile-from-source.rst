.. _binary releases: https://github.com/zrepl/zrepl/releases

.. _installation-compile-from-source:

Compile From Source
~~~~~~~~~~~~~~~~~~~

Producing a release requires **Go 1.11** or newer and **Python 3** + **pip3** + ``docs/requirements.txt`` for the Sphinx documentation.
A tutorial to install Go is available over at `golang.org <https://golang.org/doc/install>`_.
Python and pip3 should probably be installed via your distro's package manager.

::

   cd to/your/zrepl/checkout
   python3 -m venv3
   source venv3/bin/activate
   ./lazy.sh devsetup
   make release
   # build artifacts are available in ./artifacts/release

The Python venv is used for the documentation build dependencies.
If you just want to build the zrepl binary, leave it out and use `./lazy.sh godep` instead.

Alternatively, you can use the Docker build process:
it is used to produce the official zrepl `binary releases`_
and serves as a reference for build dependencies and procedure:

::

    cd to/your/zrepl/checkout
    # make sure your user has access to the docker socket
    make release-docker
    # if you want .deb or .rpm packages, invoke the follwoing
    # targets _after_ you invoked release-docker
    make deb-docker
    make rpm-docker
    # build artifacts are available in ./artifacts/release
    # packages are available in ./artifacts


.. NOTE::

    It is your job to install the built binary in the zrepl users's ``$PATH``, e.g. ``/usr/local/bin/zrepl``.
    Otherwise, the examples in the :ref:`quick-start guides <quickstart-toc>` may need to be adjusted.
