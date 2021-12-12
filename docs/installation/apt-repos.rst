
.. _installation-apt-repos:

Debian / Ubuntu APT repositories
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

We maintain APT repositories for Debian, Ubuntu and derivatives.
The fingerprint of the signing key is ``E101 418F D3D6 FBCB 9D65  A62D 7086 99FC 5F2E BF16``.
It is available at `<https://zrepl.cschwarz.com/apt/apt-key.asc>`_ .
Please open an issue in on GitHub if you encounter any issues with the repository.

::

    (
    set -ex
    zrepl_apt_key_url=https://zrepl.cschwarz.com/apt/apt-key.asc
    zrepl_apt_key_dst=/usr/share/keyrings/zrepl.gpg
    zrepl_apt_repo_file=/etc/apt/sources.list.d/zrepl.list

    # Install dependencies for subsequent commands
    sudo apt update && sudo apt install curl gnupg lsb-release

    # Deploy the zrepl apt key.
    curl -fsSL "$zrepl_apt_key_url" | tee | gpg --dearmor | sudo tee "$zrepl_apt_key_dst" > /dev/null

    # Add the zrepl apt repo.
    ARCH="$(dpkg --print-architecture)"
    CODENAME="$(lsb_release -i -s | tr '[:upper:]' '[:lower:]') $(lsb_release -c -s | tr '[:upper:]' '[:lower:]')"
    echo "Using Distro and Codename: $CODENAME"
    echo "deb [arch=$ARCH signed-by=$zrepl_apt_key_dst] https://zrepl.cschwarz.com/apt/$CODENAME main" | sudo tee /etc/apt/sources.list.d/zrepl.list

    # Update apt repos.
    sudo apt update
    )

.. NOTE::

   Until zrepl reaches 1.0, the repositories will be updated to the latest zrepl release immediately.
   This includes breaking changes between zrepl versions.
   Use ``apt-mark hold zrepl`` to prevent upgrades of zrepl.
