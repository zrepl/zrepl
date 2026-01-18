.. _installation-rpm-repos:

RPM repositories
~~~~~~~~~~~~~~~~

We provide a single RPM repository for all RPM-based Linux distros.
The zrepl binary in the repo is the same as the one published to GitHub.
Since Go binaries are statically linked, the RPM should work about everywhere.
Please open an issue on GitHub if you encounter any issues with the repository.

The fingerprint of the repo & package signing key is:
``F6F6 E8EA 6F2F 1462 2878 B5DE 50E3 4417 826E 2CE6``.
It is available at `<https://zrepl.cschwarz.com/rpm/rpm-key.asc>`_ .

.. NOTE::

   Until zrepl reaches 1.0, the repository will be updated to the latest zrepl release immediately.
   This includes breaking changes between zrepl versions.
   If that bothers you, use the `dnf versionlock plugin <https://dnf-plugins-core.readthedocs.io/en/latest/versionlock.html>`_ or ``zypper addlock zrepl`` to pin the version of zrepl on your system.



Fedora / AlmaLinux / Rocky Linux / RHEL
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^

For ``dnf``/``yum``-based distributions, copy-paste the following snippet:

.. code-block:: bash

    rpmkeys --import 'https://zrepl.cschwarz.com/rpm/rpm-key.asc'
    cat > /etc/yum.repos.d/zrepl.repo <<EOF
    [zrepl]
    name = zrepl
    baseurl = https://zrepl.cschwarz.com/rpm/repo
    gpgkey = https://zrepl.cschwarz.com/rpm/rpm-key.asc
    repo_gpgcheck = 1
    EOF
    dnf install zrepl
    # Or if you're on an older system:
    # yum install zrepl

You will be asked by dnf to verify repository metadata (``repo_gpgcheck``).
There is no way to automate that prompt.

openSUSE
^^^^^^^^

For SUSE-based distributions that use ``zypper``, copy-paste the following snippet:

.. code-block:: bash

    rpmkeys --import 'https://zrepl.cschwarz.com/rpm/rpm-key.asc'
    zypper ar --check --gpgcheck-strict --refresh https://zrepl.cschwarz.com/rpm/repo zrepl
    zypper install zrepl

