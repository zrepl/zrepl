+++
title = "Miscellaneous"
weight = 100
+++

## Runtime Directories & UNIX Sockets

zrepl daemon creates various UNIX sockets to allow communicating with it:

* the `stdinserver` transport connects to a socket named after `client_identity` parameter
* the `control` subcommand connects to a defined control socket

There is no further authentication on these sockets.
Therefore we have to make sure they can only be created and accessed by `zrepl daemon`.

In fact, `zrepl daemon` will not bind a socket to a path in a directory that is world-accessible.

The directories can be configured in the main configuration file:

```yaml
global:
  control:
    sockpath: /var/run/zrepl/control
  serve:
    stdinserver:
      sockdir: /var/run/zrepl/stdinserver
```


## Super-Verbose Job Debugging

You have probably landed here because you opened an issue on GitHub and some developer told you to do this...
So just read the annotated comments ;)

```yaml

job:
- name: ...
  ...
 # JOB DEBUGGING OPTIONS
  # should be equal for all job types, but each job implements the debugging itself
  debug:
    conn: # debug the io.ReadWriteCloser connection
      read_dump: /tmp/connlog_read   # dump results of Read() invocations to this file
      write_dump: /tmp/connlog_write # dump results of Write() invocations to this file
    rpc: # debug the RPC protocol implementation
      log: true # log output from rpc layer to the job log
```

{{% notice info %}}
Connection dumps will almost certainly contain your or other's private data. Do not share it in a bug report.
{{% /notice %}}
