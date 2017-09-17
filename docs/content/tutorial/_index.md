---
title: "Tutorial"
weight: 1
---

This tutorial shows how zrepl can be used to implement a ZFS-based pull backup.

We assume the following scenario

* Production server `prod1` with filesystems to back up
    * `zroot/var/db`
    * `zroot/usr/home` and all its child filesystems
    * **except** `zroot/usr/home/paranoid` belonging to a user doing backups themselves
* Backup server `backups` with
    * Filesystem `storage/zrepl/pull/prod1` + children dedicated to backups of `prod1`

Our backup solution should fulfill the following requirements:

* Periodically snapshot the filesystems on `prod1` *every 10 minutes*
* Incrementally replicate these snapshots to `storage/zrepl/pull/prod1/*` on `backups`
* Keep only very few snapshots on `prod1` to save disk space
* Keep a fading history (24 hourly, 30 daily, 6 monthly) of snapshots on `backups`

## Analysis

We can model this situation as two jobs:

* A **source job** on `prod1`
    * Creates the snapshots
    * Keeps a few snapshots that are also on `prod1` to enable incremental replication
* A **pull job** on `prod1`
    * Pulls the snapshots
    * Fades out snapshots as they age

{{%expand "Side note: why doesn't `backups` take the snapshots right before replication?" %}}
After all, a little `ssh prod1 'zfs snapshot...'` wouldn't be so bad, right?

As is the case with all distributed systems, the link between `prod1` and `backups` might be down for an hour or two.
We do not want to sacrifice our required backup resolution of 10 minute intervals for a temporary connection outage.

When the link comes up again, `backups` will happily catch up the 12 snapshots taken by `prod1` in the meantime, without
a gap in our backup history.
{{%/expand%}}

## Install zrepl

Follow the [OS-specific installation instructions](/install/) and come back here.

## Configure `backups`

We define a **pull job** named `pull_prod1` in the [main configuration file](/install/#main-configuration-file):

```yaml
jobs:
- name: pull_prod1
  type: pull
  connect:
    type: ssh+stdinserver
    host: prod1.example.com
    user: root
    port: 22
    identity_file: /etc/zrepl/ssh/prod1
  interval: 10m
  mapping: {
    "<":"storage/zrepl/pull/prod1"
  }
  initial_repl_policy: most_recent
  snapshot_prefix: zrepl_pull_backup_
  prune:
    policy: grid
    grid: 1x1h(keep=all) | 24x1h | 35x1d | 6x30d
```

The `connect` section instructs zrepl to use the `stdinserver` transport: instead of directly exposing zrepl on `prod1`
to the internet, `backups` starts the `zrepl stdinserver` on `prod1` via SSH.
(You can learn more about what happens [here]({{< relref "configuration/transports.md#stdinserver" >}}), or just continue following this tutorial.)

Thus, we need to create the SSH key pair `/etc/zrepl/ssh/prod1{,.pub}` and later pass the public part to `prod1`
which will use it to authenticate `backups`. Execute the following commands on `backups` as the root user:

```bash
cd /etc/zrepl
mkdir -p ssh
chmod 0700 ssh
ssh-keygen -t ed25519 -N '' -f /etc/zrepl/ssh/prod1
```
You can learn more about the [**pull job** format here]({{< relref "configuration/jobs.md#pull" >}}) but for now we are good to go.

## Configure `prod1`

We define a corresponding **source job** named `pull_backup` in the [main configuration file](/install/#main-configuration-file)
`zrepl.yml`:

```yaml
jobs:

- name: pull_backup
  type: source
  serve:
    type: stdinserver
    client_identity: backups.example.com
  datasets: {
    "zroot/var/db": "ok",
    "zroot/usr/home<": "ok",
    "zroot/usr/home/paranoid": "!",
  }
  snapshot_prefix: zrepl_pull_backup_
  interval: 10m
  prune:
    policy: grid
    grid: 1x1d(keep=all)

```

The `serve` section corresponds to the `connect` section in the configuration of `backups`.

We need to allow the SSH key on `backups` to execute `zrepl stdinserver backups.example.com` on
`prod1`. For good measure, we will in fact enforce that only this command can be executed.

Open `/root/.ssh/authorized_keys` and add either of the the following lines, replacing BACKUPS_SSH_PUBKEY at the end
of the line with the contents of `/etc/zrepl/ssh/prod1.pub` (note the **.pub** !) from `backups`.

```
# for OpenSSH >= 7.2
command="zrepl stdinserver backups.example.com",restrict BACKUPS_SSH_PUBKEY
# for older OpenSSH versions
command="zrepl stdinserver backups.example.com",no-port-forwarding,no-X11-forwarding,no-pty,no-agent-forwarding,no-user-rc  BACKUPS_SSH_PUBKEY
```

{{% alert theme="info" %}}The entries **must** be on a single line, including the replaced BACKUPS_SSH_PUBKEY{{% /alert %}}

Again, you can learn more about the [**source job** format here]({{< ref "configuration/jobs.md#source" >}}).

## Apply Configuration Changes

We need to restart the zrepl daemon on **both** `prod1` and `backups`.

This is [OS-specific](/install/#restarting).

## Watch it Work

A common setup is to watch the log output and zfs list of snapshots on both machines.

If you like tmux, here is a handy script that works on FreeBSD:

```bash
pkg install gnu-watch tmux
tmux new-window
tmux split-window "tail -f /var/log/zrepl.log"
tmux split-window "gnu-watch 'zfs list -t snapshot -o name,creation -s creation | grep zrepl_pull_backup_'"
tmux select-layout tiled
```

The Linux equivalent might look like this

```bash
# make sure tmux is installed & let's assume you use systemd + journald
tmux new-window
tmux split-window "journalctl -f -u zrepl.service"
tmux split-window "watch 'zfs list -t snapshot -o name,creation -s creation | grep zrepl_pull_backup_'"
tmux select-layout tiled
```

## Summary

Congratulations, you have a working pull backup. Where to go next?

* Read more about [configuration format, options & job types](/configuration/)
* Learn about [implementation details](/impl/) of zrepl.




