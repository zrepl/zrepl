.. _installation-rpm-repos:

RPM repositories
~~~~~~~~~~~~~~~~

Since Go binaries are statically linked, the same RPM should work on all RPM-based Linux distros.

There are 2 repositories that you can use:

#. Official zrepl Repository
#. Terra repository, which includes many other useful (unrelated) packages.

Official zrepl Repository
=========================

We provide a single RPM repository for all RPM-based Linux distros.
The zrepl binary in the repo is the same as the one published to GitHub.

The fingerprint of the signing key is ``F6F6 E8EA 6F2F 1462 2878 B5DE 50E3 4417 826E 2CE6``.
It is available at `<https://zrepl.cschwarz.com/rpm/rpm-key.asc>`_ .
Please open an issue on GitHub if you encounter any issues with the repository.

Copy-paste the following snippet into your shell to set up the zrepl repository.
Then ``dnf install zrepl`` and make sure to confirm that the signing key matches the one shown above.

::

    cat > /etc/yum.repos.d/zrepl.repo <<EOF
    [zrepl]
    name = zrepl
    baseurl = https://zrepl.cschwarz.com/rpm/repo
    gpgkey = https://zrepl.cschwarz.com/rpm/rpm-key.asc
    EOF

.. NOTE::

   Until zrepl reaches 1.0, the repository will be updated to the latest zrepl release immediately.
   This includes breaking changes between zrepl versions.
   If that bothers you, use the `dnf versionlock plugin <https://dnf-plugins-core.readthedocs.io/en/latest/versionlock.html>`_ to pin the version of zrepl on your system.

Terra Repository
================

You can find details about the Terra repository and how to use it here:
https://terra.fyralabs.com/

Quick start instructions to add the repository to your system and install zrepl:
::

    sudo dnf install --repofrompath 'terra,https://repos.fyralabs.com/terra$releasever' --setopt='terra.gpgkey=https://repos.fyralabs.com/terra$releasever/key.asc' terra-release
    sudo dnf install zrepl
