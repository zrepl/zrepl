# Tiered Snapshotting, Pruning & Replication

## Use Case

* Differently paced snapshotting & replication
    * 20 x 10min-spaced snapshots for rollback in case of adminstrative mistakes.
        * Should never be replicated
    * Daily Snapshots for off-site replication
        * at 3:00 AM to avoid impacting production workload

* Sometimes, out-of-routine snapshot for off-site replication
    * e.g. before system update


## Config Draft

```yaml
- type: push
  connect:
    type: tcp
    address: "backup-server.foo.bar:8888"
  filesystems: {
    "<": true,
    "tmp": false
  }
  send:
    snap_filters:
    - type: snapmgmt # auto-add this snap filter if snapmgmt != {} 
      # every snapmgmt method produces a list of snaps_filters which this snap_filter type represents
    - type: regex
      regex: "manual_.*" # admin may create snapshots named like `manual_pre_upgrade` and have those replicated as well
      # TODO: how does a manual `zfs snapshot` trigger replication? `zrepl signal wakeup ...` good enough for now
  snapmgmt:
    type: tiered
    prefix: zrepl_
    tiers:
    - name: local
      cron: every 10 minutes
      keep: { local: 10, remote: 0 }
    - name: daily
      cron: every day at 3 AM
      keep: { local: 14, remote: 30 }
    - name: weekly
      cron: every sunday at 3 AM
      keep: { local: 0, remote: 20 }
    - name: monthly
      cron: every last sunday of the month at 3 AM
      keep: { local: 0, remote: 12 }
```

TODO pull - likely breaks existing split of responsibilities about pruning between `pull` and `source`

## Implementation

A `snapmgmt` type implements both snapshotting and *local* pruning in one service (`RunSnapshotterAndLocalPruner()`)
The `snapmgmt` type `tiered` works as follows:

* Snapshots are created per tier:
  * snapshot name = `${PREFIX}_${JOBNAME}_TIERED_${thistier.name}_${NOW}`
  * property `zrepl:JOBNAME:tier=${thistier.name}`
  * property `zrepl:JOBNAME:destroy_at=${thistier.destroy_at(NOW)}`
* Sender-side pruning by
  * filtering all snaps by userprop `zrepl:JOBNAME:tier=${thistier.name}`
  * destroying all snaps in the filtered list with `(zrepl:JOBNAME:destroy_at).Before(NOW)`
  * sleeping until MIN of
    * `MIN(filtered_list.zrepl:JOBNAME:destroy_at)`
    * `(<-newSnapshots).DestroyAt)`

The replication enigne remains orthogonal to `snapmgmt`, but the protocol allows a sender to present a filtered view of snapshots in the `ListFilesystemVersions` RPC response.
The filters are specified in a list, and have OR semantics, i.e., if one filter matches, the snapshot is presented tothe replication engine.
The filter type supporting the feature proposed in this issue is the `tiered` filter type:
it includes snapshots that are
* named by the snapshotter above: `${PREFIX}_${JOBNAME}_${snapmgmt.tiers|.name}_${NOW}`
* and the respective tier has a non-zero `keep.remote`
* TODO: deriving the tier from the snapshot name is unclean.
  We could instead allow that a replication filter is not pure (i.e. may use zfs.ZFSGet).
  Would work for both `push` and `source` jobs.

Receiver-side pruning is still unsolved (TODO):

* We could replicate the `destroy_at` property, and have independent pruning on the receive-side.
    * While there are cases where this is desirable (use case "guarantee that replicated data is destroyed after X months")
    * it might be undesirable in cases where the receive-side is a backup (which should not self-destroy)
* We could require each `snapmgmt` method to have a `PruneReceiver(receiver)` method, and require that `receiver` provides an RPC endpoint to get the `destroy_at` infromation required to evaluate the prune policy (e.g. a flag in the request, like `GetDestroyAt=true`)

## Other future `snapmgmt` methods

```yaml
snapmgmt:
  # zrepl signal ondemand JOBNAME SNAPNAME EXPIRE_AT
  # => creates snapshot @SNAPNAME for all filesystems matched by `$JOB.filesystems` with expiration date `EXPIRE_AT`
  # => snapshot has user prop `zrepl:JOBNAME:ondemand=on`
  # replication filter `snapmgmt` filters by `ondemand` property
  # pruning filter uses expieration data encoded in property
  type: ondemand

snapmgmt:
  # no snapshots, never-matching filter
  type: manual
```

## Existing Use-Cases

### Existing Configurations & Migration to New Config

`snapmgmt` replaces the previous `snapshotting` and `pruning` fields.
Proposed migration for existing configs:

```yaml
snapmgmt:
  type: pre-snapmgmt
  snapshotting:
    type: periodic
    interval: 10m
    prefix: zrepl_
  pruning:
    keep_sender:
      - type: not_replicated
      - type: last_n
        count: 10
      - type: grid
        grid: 1x1h(keep=all) | 24x1h | 14x1d
        regex: "^zrepl_.*"
    keep_receiver:
      - type: grid
        grid: 1x1h(keep=all) | 24x1h | 35x1d | 6x30d
        regex: "^zrepl_.*"
```

