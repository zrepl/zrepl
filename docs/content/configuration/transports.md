+++
title = "Transports"
weight = 30
+++

{{% alert theme="warning" %}}Under Construction{{% /alert %}}

## Stdinserver


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

