.. include:: global.rst.inc

.. _quickstart-toc:

***********************
Quick Start by Use Case
***********************

The goal of this quick-start guide is to give you an impression of how zrepl can accomodate your use case.

Install zrepl
=============

Follow the :ref:`OS-specific installation instructions <installation_toc>` and come back here.

Overview Of How zrepl Works
============================

Check out the :ref:`overview section <job-overview>` to get a rough idea of what you are going to configure in the next step, then come back here.

Configuration Examples
======================

zrepl is configured through a YAML configuration file in ``/etc/zrepl/zrepl.yml``.
We have prepared example use cases that show-case typical deployments and different functionality of zrepl.
We encourage you to read through all of the examples to get an idea of what zrepl has to offer, and how you can mix-and-match configurations for your use case.
Keep the :ref:`full config documentation <configuration_toc>` handy if a config snippet is unclear.

**Example Use Cases**

.. toctree::
   :titlesonly:

   quickstart/continuous_server_backup
   quickstart/backup_to_external_disk

Use ``zrepl configcheck`` to validate your configuration.
No output indicates that everything is fine.

.. NOTE::

   Please open an issue on GitHub if your use case for zrepl is significantly different from those listed above.
   Or even better, write it up in the same style as above and open a PR!

.. _quickstart-apply-config:

Apply Configuration Changes
===========================

We hope that you have found a configuration that fits your use case.
Use ``zrepl configcheck`` once again to make sure the config is correct (output indicates that everything is fine).
Then restart the zrepl daemon on all systems involved in the replication, likely using ``service zrepl restart`` or ``systemctl restart zrepl``.

Watch it Work
=============

Run ``zrepl status`` on the active side of the replication setup to monitor snaphotting, replication and pruning activity.
To re-trigger replication (snapshots are separate!), use ``zrepl signal wakeup JOBNAME``.
(refer to the example use case document if you are uncertain which job you want to wake up).

You can also use basic UNIX tools to inspect see what's going on.
If you like tmux, here is a handy script that works on FreeBSD: ::

    pkg install gnu-watch tmux
    tmux new -s zrepl -d
    tmux split-window -t zrepl "tail -f /var/log/messages"
    tmux split-window -t zrepl "gnu-watch 'zfs list -t snapshot -o name,creation -s creation'"
    tmux split-window -t zrepl "zrepl status"
    tmux select-layout -t zrepl tiled
    tmux attach -t zrepl

The Linux equivalent might look like this: ::

    # make sure tmux is installed & let's assume you use systemd + journald
    tmux new -s zrepl -d
    tmux split-window -t zrepl  "journalctl -f -u zrepl.service"
    tmux split-window -t zrepl "watch 'zfs list -t snapshot -o name,creation -s creation'"
    tmux split-window -t zrepl "zrepl status"
    tmux select-layout -t zrepl tiled
    tmux attach -t zrepl

What Next?
==========

* Read more about :ref:`configuration format, options & job types <configuration_toc>`
* Configure :ref:`logging <logging>` \& :ref:`monitoring <monitoring>`.

