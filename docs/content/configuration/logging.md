+++
title = "Logging"
weight = 90
+++

zrepl uses structured logging to provide users with easily processable log messages.

## Configuration

Logging outlets are configured in the `global` section of the [configuration file]({{< relref "install/_index.md#configuration-files" >}}).<br />
Check out {{< sampleconflink "random/logging.yml" >}} for an example on how to configure multiple outlets:

```yaml
global:
  logging:

    - outlet: OUTLET_TYPE
      level: MINIMUM_LEVEL
      format: FORMAT

    - outlet: OUTLET_TYPE
      level: MINIMUM_LEVEL
      format: FORMAT

    ...

jobs: ...

```

### Default Configuration

By default, the following logging configuration is used

```yaml
global:
  logging:

    - outlet: "stdout"
      level:  "warn"
      format: "human"
```

{{% notice info %}}
Output to **stderr** should always be considered a **critical error**.<br />
Only errors in the logging infrastructure itself, e.g. IO errors when writing to an outlet, are sent to stderr.
{{% / notice %}}

## Building Blocks

The following sections document the semantics of the different log levels, formats and outlet types.

### Levels

| Level | SHORT | Description |
|-------|-------|-------------|
|`error`|`ERRO` | immediate action required |
|`warn` |`WARN` | symptoms for misconfiguration, soon expected failure, etc.|
|`info` |`INFO` | explains what happens without too much detail |
|`debug`|`DEBG` | tracing information, state dumps, etc. useful for debugging. |

Incorrectly classified messages are considered a bug and should be reported.

### Formats

| Format | Description |
|--------|---------|
|`human` | emphasized context by putting job, task, step and other context variables into brackets before the actual message, followed by remaining fields in logfmt style|
|`logfmt`| [logfmt](https://brandur.org/logfmt) output. zrepl uses [github.com/go-logfmt/logfmt](github.com/go-logfmt/logfmt).|
|`json`  | JSON formatted output. Each line is a valid JSON document. Fields are marshaled by `encoding/json.Marshal()`, which is particularly useful for processing in log aggregation or when processing state dumps.

### Outlets

Outlets are ... well ... outlets for log entries into the world.

#### **`stdout`**

| Parameter | Default   | Comment |
|-----------| --------- | ----------- |
|`outlet`   | *none*    | required |
|`level`    | *none*    | minimum [log level](#levels), required |
|`format`   | *none*    | output [format](#formats), required |

Writes all log entries with minimum level `level` formatted by `format` to stdout.

Can only be specified once.

#### **`syslog`**

| Parameter | Default   | Comment |
|-----------| --------- | ----------- |
|`outlet`   | *none*    | required |
|`level`    | *none*    | minimum [log level](#levels), required, usually `debug` |
|`format`   | *none*    | output [format](#formats), required|
|`retry_interval`| 0 | Interval between reconnection attempts to syslog  |

Writes all log entries formatted by `format` to syslog.
On normal setups, you should not need to change the `retry_interval`.

Can only be specified once.

#### **`tcp`**

| Parameter | Default   | Comment |
|-----------| --------- | ----------- |
|`outlet`   | *none*    | required |
|`level`    | *none*    | minimum [log level](#levels), required |
|`format`   | *none*    | output [format](#formats), required |
|`net`|*none*|`tcp` in most cases|
|`address`|*none*|remote network, e.g. `logs.example.com:10202`|
|`retry_interval`|*none*|Interval between reconnection attempts to `address`|
|`tls`|*none*|TLS config (see below)|

Establishes a TCP connection to `address` and sends log messages with minimum level `level` formatted by `format`.

If `tls` is not specified, an unencrypted connection is established.

If `tls` is specified, the TCP connection is secured with TLS + Client Authentication.
This is particularly useful in combination with log aggregation services that run on an other machine.

|Parameter|Description|
|---------|-----------|
|`ca`|PEM-encoded certificate authority that signed the remote server's TLS certificate|
|`cert`|PEM-encoded client certificate identifying this zrepl daemon toward the remote server|
|`key`|PEM-encoded, unencrypted client private key identifying this zrepl daemon toward the remote server|

{{% notice note %}}
zrepl uses Go's `crypto/tls` and `crypto/x509` packages and leaves all but the required fields in `tls.Config` at their default values.
In case of a security defect in these packages, zrepl has to be rebuilt because Go binaries are statically linked.
{{% / notice %}}
