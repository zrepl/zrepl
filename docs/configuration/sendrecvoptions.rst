.. include:: ../global.rst.inc


Send & Recv Options
===================

.. _job-send-options:

Send Options
~~~~~~~~~~~~

:ref:`Source<job-source>` and :ref:`push<job-push>` jobs have an optional ``send`` configuration section.

::

   jobs:
   - type: push
     filesystems: ...
     send:
       # flags from the table below go here
     ...

The following table specifies the list of (boolean) options.
Flags with an entry in the ``zfs send`` column map directly to the zfs send CLI flags.
zrepl does not perform feature checks for these flags.
If you enable a flag that is not supported by the installed version of ZFS, the zfs error will show up at runtime in the logs and zrepl status.
See the `upstream man page <https://openzfs.github.io/openzfs-docs/man/8/zfs-send.8.html>`_ (``man zfs-send``) for their semantics.

.. list-table::
    :widths: 20 10 70
    :header-rows: 1

    * - ``send.``
      - ``zfs send``
      - Comment
    * - ``encrypted``
      -
      - Specific to zrepl, :ref:`see below <job-send-options-encrypted>`.
    * - ``bandwidth_limit``
      -
      - Specific to zrepl, :ref:`see below <job-send-recv-options--bandwidth-limit>`.
    * - ``raw``
      - ``-w``
      - Use ``encrypted`` to only allow encrypted sends. Mixed sends are not supported.
    * - ``send_properties``
      - ``-p``
      - **Be careful**, read the :ref:`note on property replication below <job-note-property-replication>`.
    * - ``backup_properties``
      - ``-b``
      - **Be careful**, read the :ref:`note on property replication below <job-note-property-replication>`.
    * - ``large_blocks``
      - ``-L``
      - **Potential data loss on OpenZFS < 2.0**, see :ref:`warning below <job-send-options-large-blocks>`.
    * - ``compressed``
      - ``-c``
      -
    * - ``embedded_data``
      - ``-e``
      -
    * - ``saved``
      - ``-S``
      -

.. _job-send-options-encrypted:

``encrypted``
-------------

The ``encrypted`` option controls whether the matched filesystems are sent as `OpenZFS native encryption <http://open-zfs.org/wiki/ZFS-Native_Encryption>`_ raw sends.
More specifically, if ``encrypted=true``, zrepl

* checks for any of the filesystems matched by ``filesystems`` whether the ZFS ``encryption`` property indicates that the filesystem is actually encrypted with ZFS native encryption and
* invokes the ``zfs send`` subcommand with the ``-w`` option (raw sends) and
* expects the receiving side to support OpenZFS native encryption (recv will fail otherwise)

Filesystems matched by ``filesystems`` that are not encrypted are not sent and will cause error log messages.

If ``encrypted=false``, zrepl expects that filesystems matching ``filesystems`` are not encrypted or have loaded encryption keys.

.. NOTE::

   Use ``encrypted`` instead of ``raw`` to make your intent clear that zrepl must only replicate filesystems that are actually encrypted by OpenZFS native encryption.
   It is meant as a safeguard to prevent unintended sends of unencrypted filesystems in raw mode.

.. _job-send-options-properties:

``send_properties``
-------------------
Sends the dataset properties along with snapshots.
Please be careful with this option and read the :ref:`note on property replication below <job-note-property-replication>`.

.. _job-send-options-backup-properties:

``backup_properties``
---------------------

When properties are modified on a filesystem that was received from a send stream with ``send.properties=true``, ZFS archives the original received value internally.
This also applies to :ref:`inheriting or overriding properties during zfs receive <job-recv-options--inherit-and-override>`.

When sending those received filesystems another hop, the ``backup_properties`` flag instructs ZFS to send the original property values rather than the current locally set values.

This is useful for replicating properties across multiple levels of backup machines.
**Example:**
Suppose we want to flow snapshots from Machine A to B, then from B to C.
A will enable the :ref:`properties send option <job-send-options-properties>`.
B will want to override :ref:`critical properties such as mountpoint or canmount <job-note-property-replication>`.
But the job that replicates from B to C should be sending the original property values received from A.
Thus, B sets the ``backup_properties`` option.

Please be careful with this option and read the :ref:`note on property replication below <job-note-property-replication>`.

.. _job-send-options-large-blocks:

``large_blocks``
----------------

This flag should not be changed after initial replication.
Prior to `OpenZFS commit 7bcb7f08 <https://github.com/openzfs/zfs/pull/10383/files#diff-4c1e47568f46fb63546e984943b09a3e6b051e2242649523f7835bbdfe2a9110R337-R342>`_
it was possible to change this setting which resulted in **data loss on the receiver**.
The commit in question is included in OpenZFS 2.0 and works around the problem by prohibiting receives of incremental streams with a flipped setting.

.. WARNING::

   This bug has **not been fixed in the OpenZFS 0.8 releases** which means that changing this flag after initial replication might cause **data loss** on the receiver.

.. _job-recv-options:

Recv Options
~~~~~~~~~~~~

:ref:`Sink<job-sink>` and :ref:`pull<job-pull>` jobs have an optional ``recv`` configuration section:

::

   jobs:
   - type: pull
     recv:
       properties:
         inherit:
           - "mountpoint"
         override: {
           "org.openzfs.systemd:ignore": "on"
         }
       bandwidth_limit: ...
       placeholder:
         encryption: unspecified | off | inherit
     ...

Jump to
:ref:`properties <job-recv-options--inherit-and-override>` ,
:ref:`bandwidth_limit <job-send-recv-options--bandwidth-limit>` , and
:ref:`placeholder <job-recv-options--placeholder>`.

.. _job-recv-options--inherit-and-override:

``properties``
--------------

``override`` maps directly to the `zfs recv -o flag <https://openzfs.github.io/openzfs-docs/man/8/zfs-recv.8.html>`_.
Property name-value pairs specified in this map will apply to all received filesystems, regardless of whether the send stream contains properties or not.

``inherit`` maps directly to the `zfs recv -x flag <https://openzfs.github.io/openzfs-docs/man/8/zfs-recv.8.html>`_.
Property names specified in this list will be inherited from the receiving side's parent filesystem (e.g. ``root_fs``).

With both options, the sending side's property value is still stored on the receiver, but the local override or inherit is the one that takes effect.
You can send the original properties from the first receiver to another receiver using :ref:`send.backup_properties<job-send-options-backup-properties>`.


.. _job-note-property-replication:

A Note on Property Replication
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

If a send stream contains properties, as per ``send.send_properties`` or ``send.backup_properties``,
the default ZFS behavior is to use those properties on the receiving side, verbatim.

In many use cases for zrepl, this can have devastating consequences.
For example, when backing up a filesystem that has ``mountpoint=/`` to a storage server,
that storage server's root filesystem will be shadowed by the received file system on some platforms.
Also, many scripts and tools use ZFS user properties for configuration and do not check the property source (``local`` vs. ``received``).
If they are installed on the receiving side as well as the sending side, property replication could have unintended effects.

**zrepl currently does not provide any automatic safe-guards for property replication:**

* Make sure to read the entire man page on zfs recv (`man zfs recv <https://openzfs.github.io/openzfs-docs/man/8/zfs-recv.8.html>`_) before enabling this feature.
* Use ``recv.properties.override`` whenever possible, e.g. for ``mountpoint=none`` or ``canmount=off``.
* Use ``recv.properties.inherit`` if that makes more sense to you.

Below is an **non-exhaustive list of problematic properties**.
Please open a pull request if you find a property that is missing from this list.
(Both with regards to core ZFS tools and other software in the broader ecosystem.)

Mount behaviour
---------------

* ``mountpoint``
* ``canmount``
* ``overlay``

Note: Before `OpenZFS 2.0.5 <https://github.com/openzfs/zfs/issues/11416>`_, inheriting or overriding the ``mountpoint`` property on ZVOLs fails in ``zfs recv``.
If you are on such an older version, consider creating separate zrepl jobs for your ZVOL and filesystem datasets.

Systemd
-------

With systemd, you should also consider the properties processed by the `zfs-mount-generator <https://manpages.debian.org/buster-backports/zfsutils-linux/zfs-mount-generator.8.en.html>`_ .

Most notably:

* ``org.openzfs.systemd:ignore``
* ``org.openzfs.systemd:wanted-by``
* ``org.openzfs.systemd:required-by``

Encryption
----------

If the sender filesystems are encrypted but the sender does :ref:`plain sends <zfs-background-knowledge-plain-vs-raw-sends>`
and property replication is enabled, the receiver must :ref:`inherit the following properties<job-recv-options--inherit-and-override>`:

* ``keylocation``
* ``keyformat``
* ``encryption``

Sharing
-------

You may not want the replicated filesystem shared in the same way as the source is.

* ``sharenfs``
* ``sharesmb``

.. _job-recv-options--placeholder:

Placeholders
~~~~~~~~~~~~

::

   placeholder:
     encryption: unspecified | off | inherit

During replication, zrepl :ref:`creates placeholder datasets <replication-placeholder-property>` on the receiving side if the sending side's ``filesystems`` filter creates gaps in the dataset hierarchy.
This is generally fully transparent to the user.
However, with OpenZFS Native Encryption, placeholders require zrepl user attention.
Specifically, the problem is that, when zrepl attempts to create the placeholder dataset on the receiver, and that placeholder's parent dataset is encrypted, ZFS wants to inherit encryption to the placeholder.
This is relevant to two use cases that zrepl supports:

1. **encrypted-send-to-untrusted-receiver** In this use case, the sender sends an :ref:`encrypted send stream <job-send-options-encrypted>` and the receiver doesn't have the key loaded.
2. **send-plain-encrypt-on-receive** The receive-side ``root_fs`` dataset is encrypted, and the senders are unencrypted.
   The key of ``root_fs`` is loaded, and the goal is that the plain sends (e.g., from production) are encrypted on-the-fly during receive, with ``root_fs``'s key.

For **encrypted-send-to-untrusted-receiver**, the placeholder datasets need to be created with ``-o encryption=off``.
Without it, creation would fail with an error, indicating that the placeholder's parent dataset's key needs to be loaded.
But we don't trust the receiver, so we can't expect that to ever happen.

However, for **send-plain-encrypt-on-receive**, we cannot set ``-o encryption=off``.
The reason is that if we did, any of the (non-placeholder) child datasets below the placeholder would inherit ``encryption=off``, thereby silently breaking our encrypt-on-receive use case.
So, to cover this use case, we need to create placeholders without specifying ``-o encryption``.
This will make ``zfs create`` inherit the encryption mode from the parent dataset, and thereby transitively from ``root_fs``.

The zrepl config provides the `recv.placeholder.encryption` knob to control this behavior.
In ``undefined`` mode (default), placeholder creation bails out and asks the user to configure a behavior.
In ``off`` mode, the placeholder is created with ``encryption=off``, i.e., **encrypted-send-to-untrusted-rceiver** use case.
In ``inherit`` mode, the placeholder is created without specifying ``-o encryption`` at all, i.e., the **send-plain-encrypt-on-receive** use case.


Common Options
~~~~~~~~~~~~~~

.. _job-send-recv-options--bandwidth-limit:

Bandwidth Limit (send & recv)
-----------------------------

::

   bandwidth_limit:
     max: 23.5 MiB # -1 is the default and disabled rate limiting
     bucket_capacity: # token bucket capacity in bytes; defaults to 128KiB

Both ``send`` and ``recv`` can be limited to a maximum bandwidth through ``bandwidth_limit``.
For most users, it should be sufficient to just set ``bandwidth_limit.max``.
The ``bandwidth_limit.bucket_capacity`` refers to the `token bucket size <https://github.com/juju/ratelimit>`_.

The bandwidth limit only applies to the payload data, i.e., the ZFS send stream.
It does not account for transport protocol overheads.
The scope is the job level, i.e., all :ref:`concurrent <replication-option-concurrency>` sends or incoming receives of a job share the bandwidth limit.
