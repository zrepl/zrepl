+++
title = "Overview"
weight = 100
description = "Configuration format, SSH authentication, etc."
+++

{{% panel header="Recommendation" %}}
Keep the [sample configuration file](https://github.com/zrepl/zrepl/blob/master/cmd/sampleconf/zrepl.yml) open on the side while reading this document!
{{% / panel %}}

All configuration is managed in a single YAML file.<br />
It is structured by sections roughly corresponding to `zrepl` subcommands:

```yaml
# REPLICATION
# Remote zrepl instances where pull and push jobs connect to
remotes: 
  name_of_remote: #...
# Push jobs (replication from local to remote)
pushs: 
  name_of_push_job: #...
  name_of_other_push_job: #...
# pull jobs (replication from remote to local & local to local)
pulls: 
  name_of_pull_job: #...
# mapping incoming pushs to local datasets
sinks: 
  client_identity: #...
# access control for remote pull jobs
pull_acls: 
  client_identity: #...

# SNAPSHOT MANAGEMENT
# Automatic snapshotting of filesystems
autosnap:
  name_of_autosnap_job: #... 
# Automatic pruning of snapshots based on creation date
prune:
  name_of_prune_job: #... 
```

When using `zrepl(8)`, a *subcommand* is passed the *job name* as a positional argument:

```yaml
autosnap: # subcommand
  db: # job name
    prefix: zrepl_ 
    interval: 10m
    dataset_filter: {
      "tank/db<": ok
    }
```
```bash
$ zrepl autosnap --config zrepl.yml db
```

Run `zrepl --help` for a list of subcommands and options.

## Mapping & Filter Syntax

For various job types, a filesystem `mapping` or `filter` needs to be
specified.

Both have in common that they return a result for a given filesystem path (in
the ZFS filesystem hierarchy): mappings return a *target filesystem*, filters
return a *filter result* (`omit`, `ok`).

The pattern syntax is the same for mappings and filters and is documented in
the following section.

#### Pattern Syntax

A mapping / filter is specified as a **YAML dictionary** with patterns as keys and
results as values.<br />
The following rules determine which result is chosen for a given filesystem path:

* More specific path patterns win over less specific ones
* Non-wildcard patterns (full path patterns) win over *subtree wildcards* (`<` at end of pattern)

{{% panel %}}
The **subtree wildcard** `<` means "*the dataset left of `<` and all its children*".
{{% / panel %}}

##### Example

```
# Rule number and its pattern
1: tank<            # tank and all its children
2: tank/foo/bar     # full path pattern (no wildcard)
3: tank/foo<        # tank/foo and all its children

# Which rule applies to given path?
tank/foo/bar/loo => 3
tank/bar         => 1
tank/foo/bar     => 2
zroot            => NO MATCH
tank/var/log     => 1
```

#### Mappings

The example below shows a pull job that would pull remote datasets to the given local paths.<br />
If there exists no mapping (`NO MATCH`), the filesystem will not be pulled.

```yaml
pull:
  app01.example.com: # client identity
    from: app01.example.com
    mapping: { 
      "tank/var/db<":    "zroot/backups/app01/tank/var/db",
      "tank/var/www<":   "zroot/backups/app01/tank/var/www",
      "tank/var/log":    "zroot/logbackup/app01",
    }
```

```
tank/var/db`          => zroot/backups/app01/tank/var/db
tank/var/db/a/child   => zroot/backups/app01/tank/var/db/a/child
...
tank/var/www          => zroot/backups/app01/tank/var/www
tank/var/www/a/child  => zroot/backups/app01/tank/var/www/a/child
...
tank/var/log          => zroot/logbackup/app01
tank/var/log/foo      => NOT MAPPED
```

#### Filters

Valid filter results: `ok` or `omit`.

The example below shows a pull ACL that allows access to the user homes but not
to the rest of the system's datasets.

```
# Example for filter syntax
pull_acls:
  backups.example.com: # client identity
    filter: {
      "<":              omit,
      "tank/usr/home<": ok,
    }
```

## Next up

* [Automating snapshot creation & pruning]({{< ref "configuration/snapshots.md" >}})

* [Replicating filesystems]({{< ref "configuration/replication.md" >}})

