.. _installation-rpm-repos:

RPM repositories
~~~~~~~~~~~~~~~~

We provide a single RPM repository for all RPM-based Linux distros.
The zrepl binary in the repo is the same as the one published to GitHub.
Since Go binaries are statically linked, the RPM should work about everywhere.

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
    repo_gpgcheck = 1
    EOF

If you are on openSUSE, adapt the spec path accordingly, i.e. replace ``/etc/yum.repos.d/zrepl.repo`` with ``/etc/zypp/repos.d/zrepl.repo``.
Then ``zypper install zrepl``, confirming that the signing key matches.
The RPM package has been successfully used on Tumbleweed and MicroOS.

.. NOTE::

   Until zrepl reaches 1.0, the repository will be updated to the latest zrepl release immediately.
   This includes breaking changes between zrepl versions.
   If that bothers you, use the `dnf versionlock plugin <https://dnf-plugins-core.readthedocs.io/en/latest/versionlock.html>`_ or ``zypper addlock zrepl`` to pin the version of zrepl on your system.
