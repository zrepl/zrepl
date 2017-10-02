+++
title = "Mapping & Filter Syntax"
weight = 20
description = "How to specify mappings & filters"
+++

## Mapping & Filter Syntax

For various job types, a filesystem `mapping` or `filter` needs to be
specified.

Both have in common that they take a filesystem path (in the ZFS filesystem hierarchy)as parameters and return something.
Mappings return a *target filesystem* and filters return a *filter result*.

The pattern syntax is the same for mappings and filters and is documented in the following section.

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

Mappings map a *filesystem path* to a *target filesystem*.
Per pattern, either a target filesystem path or `"!"` is specified as a result.

* If no pattern matches, there exists no target filesystem (`NO MATCH`).
* If the result is a `"!"`, there exists no target filesystem (`NO MATCH`).
* If the pattern is a non-wildcard pattern, the filesystem specified on the left is mapped to the target filesystem on the right.
* If the pattern is a *subtree wildcard* pattern, the root of the subtree specified in the pattern is mapped to the target filesystem on the right and all children are mapped bewlow it.

Note that paths are never appended - a mapping represents a correspondence between a path on the left and a path on the right.

The example is from the {{< sampleconflink "localbackup/host1.yml" >}} example config.

```yaml
jobs:
- name: mirror_local
  type: local
  mapping: {
    "zroot/var/db<":    "storage/backups/local/zroot/var/db",
    "zroot/usr/home<":  "storage/backups/local/zroot/usr/home",
    "zroot/usr/home/paranoid":  "!", #don't backup paranoid user
    "zroot/poudriere/ports<": "!", #don't backup the ports trees
  }
  ...
```

```
zroot/var/db                        => storage/backups/local/zroot/var/db
zroot/var/db/a/child                => storage/backups/local/zroot/var/db/a/child
zroot/usr/home                      => storage/backups/local/zroot/usr/home
zroot/usr/home/paranoid             => NOT MAPPED
zroot/usr/home/bob                  => storage/backups/local/zroot/usr/home/bob
zroot/usr/src                       => NOT MAPPED
zroot/poudriere/ports/2017Q3        => NOT MAPPED
zroot/poudriere/ports/HEAD          => NOT MAPPED
```

#### Filters

Valid filter results: `ok` or `!`.

The example below show the source job from the [tutorial]({{< relref "tutorial/_index.md#configure-app-srv" >}}):

The client is allowed access to `zroot/var/db`, `zroot/usr/home` + children except `zroot/usr/home/paranoid`.

```yaml
jobs:
- name: pull_backup
  type: source
  ...
  datasets: {
    "zroot/var/db": "ok",
    "zroot/usr/home<": "ok",
    "zroot/usr/home/paranoid": "!",
  }
  ...
```
