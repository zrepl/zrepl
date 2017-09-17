+++
title = "Mapping & Filter Syntax"
weight = 20
description = "How to specify mappings & filters"
+++

{{% alert theme="warning" %}}Under Construction{{% /alert %}}

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
