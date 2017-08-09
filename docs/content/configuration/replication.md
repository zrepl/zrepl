+++
title = "Filesystem Replication"
description = "Replicating filesystems with existing bookmarks & snapshots"
weight = 300
+++

{{% alert theme="warning"%}}Under Construction{{% /alert %}}

### Remotes

The `remotes` section specifies remote `zrepl` instances from which to pull from / push backups to:

```yaml
remotes:
  offsite_backups:
    transport:
      ssh:
        host: 192.168.122.6
        user: root
        port: 22
        identity_file: /etc/zrepl/identities/offsite_backups
```

#### SSH Transport

The SSH transport connects to the remote server using the SSH binary in
`$PATH` and the parameters specified in the `zrepl` config file.

However, instead of a traditional interactive SSH session, `zrepl` expects
another instance of `zrepl` on the other side of the connection; You may be
familiar with this concept from [git shell](https://git-scm.com/docs/git-shell)
or [Borg Backup](https://borgbackup.readthedocs.io/en/stable/deployment.html).

Check the examples for instructions on how to set this up on your machines! 

{{% panel %}}
The environment variables of the underlying SSH process are cleared. `$SSH_AUTH_SOCK` will not be available. We suggest creating a separate, unencrypted SSH key.
{{% / panel %}}


