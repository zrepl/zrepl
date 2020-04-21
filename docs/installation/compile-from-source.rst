.. _binary releases: https://github.com/zrepl/zrepl/releases

.. _installation-compile-from-source:

Compile From Source
~~~~~~~~~~~~~~~~~~~

Producing a release requires **Go 1.11** or newer and **Python 3** + **pip3** + ``docs/requirements.txt`` for the Sphinx documentation.
A tutorial to install Go is available over at `golang.org <https://golang.org/doc/install>`_.
Python and pip3 should probably be installed via your distro's package manager.

Alternatively, you can use the Docker build process:
it is used to produce the official zrepl `binary releases`_
and serves as a reference for build dependencies and procedure:

::

    git clone https://github.com/zrepl/zrepl.git && \
    cd zrepl && \
    sudo docker build -t zrepl_build -f build.Dockerfile . && \
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

    It is your job to install the appropriate binary in the zrepl users's ``$PATH``, e.g. ``/usr/local/bin/zrepl``.
    Otherwise, the examples in the :ref:`tutorial` may need to be adjusted.
