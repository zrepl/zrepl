
.. _installation-apt-repos:

Debian / Ubuntu APT repositories
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

We maintain APT repositories for Debian, Ubuntu and derivatives.
The fingerprint of the signing key is ``E101 418F D3D6 FBCB 9D65  A62D 7086 99FC 5F2E BF16``.
It is available at `<https://zrepl.cschwarz.com/apt/apt-key.asc>`_ .
Please open an issue in on GitHub if you encounter any issues with the repository.

The following snippet configure the repository for your Debian or Ubuntu release:

::

    sudo apt update && sudo apt install curl gnupg lsb-release; \
    ARCH="$(dpkg --print-architecture)"; \
    CODENAME="$(lsb_release -i -s | tr '[:upper:]' '[:lower:]') $(lsb_release -c -s | tr '[:upper:]' '[:lower:]')"; \
    echo "Using Distro and Codename: $CODENAME"; \
    (curl https://zrepl.cschwarz.com/apt/apt-key.asc | sudo apt-key add -) && \
    (echo "deb [arch=$ARCH] https://zrepl.cschwarz.com/apt/$CODENAME main" | sudo tee /etc/apt/sources.list.d/zrepl.list) && \
    sudo apt update


.. NOTE::

   Until zrepl reaches 1.0, the repositories will be updated to the latest zrepl release immediately.
   This includes breaking changes between zrepl versions.
   Use ``apt-mark hold zrepl`` to prevent upgrades of zrepl.
