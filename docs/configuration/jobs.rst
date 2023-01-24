.. include:: ../global.rst.inc

.. _job:

Job Types in Detail
===================

.. _job-push:

Job Type ``push``
-----------------

.. list-table::
    :widths: 20 80
    :header-rows: 1

    * - Parameter
      - Comment
    * - ``type``
      - = ``push``
    * - ``name``
      - unique name of the job :issue:`(must not change)<327>`
    * - ``connect``
      - |connect-transport|
    * - ``filesystems``
      - |filter-spec| for filesystems to be snapshotted and pushed to the sink
    * - ``send``
      - |send-options| 
    * - ``snapshotting``
      - |snapshotting-spec|
    * - ``pruning``
      - |pruning-spec|
    * - ``replication``
      - |replication-options|
    * - ``conflict_resolution``
      - |conflict-resolution-options|

Example config: :sampleconf:`/push.yml`

.. _job-sink:

Job Type ``sink``
-----------------

.. list-table::
    :widths: 20 80
    :header-rows: 1

    * - Parameter
      - Comment
    * - ``type``
      - = ``sink``
    * - ``name``
      - unique name of the job :issue:`(must not change)<327>`
    * - ``serve``
      - |serve-transport|
    * - ``root_fs``
      - ZFS filesystems are received to
        ``$root_fs/$client_identity/$source_path``

Example config: :sampleconf:`/sink.yml`

.. _job-pull:

Job Type ``pull``
-----------------

.. list-table::
    :widths: 20 80
    :header-rows: 1

    * - Parameter
      - Comment
    * - ``type``
      - = ``pull``
    * - ``name``
      - unique name of the job :issue:`(must not change)<327>`
    * - ``connect``
      - |connect-transport|
    * - ``root_fs``
      - ZFS filesystems are received to
        ``$root_fs/$source_path``
    * - ``interval``
      - | Interval at which to pull from the source job (e.g. ``10m``).
        | ``manual`` disables periodic pulling, replication then only happens on :ref:`wakeup <cli-signal-wakeup>`.
    * - ``pruning``
      - |pruning-spec|
    * - ``replication``
      - |replication-options|
    * - ``conflict_resolution``
      - |conflict-resolution-options|

Example config: :sampleconf:`/pull.yml`

.. _job-source:

Job Type ``source``
-------------------

.. list-table::
    :widths: 20 80
    :header-rows: 1

    * - Parameter
      - Comment
    * - ``type``
      - = ``source``
    * - ``name``
      - unique name of the job :issue:`(must not change)<327>`
    * - ``serve``
      - |serve-transport|
    * - ``filesystems``
      - |filter-spec| for filesystems to be snapshotted and exposed to connecting clients
    * - ``send``
      - |send-options| 
    * - ``snapshotting``
      - |snapshotting-spec|

Example config: :sampleconf:`/source.yml`


.. _replication-local:

Local replication
-----------------

If you have the need for local replication (most likely between two local storage pools), you can use the :ref:`local transport type <transport-local>` to connect a local push job to a local sink job.

Example config: :sampleconf:`/local.yml`.


.. _job-snap:

Job Type ``snap`` (snapshot & prune only)
-----------------------------------------

Job type that only takes snapshots and performs pruning on the local machine.

.. list-table::
    :widths: 20 80
    :header-rows: 1

    * - Parameter
      - Comment
    * - ``type``
      - = ``snap``
    * - ``name``
      - unique name of the job :issue:`(must not change)<327>`
    * - ``filesystems``
      - |filter-spec| for filesystems to be snapshotted
    * - ``snapshotting``
      - |snapshotting-spec|
    * - ``pruning``
      - |pruning-spec|

Example config: :sampleconf:`/snap.yml`
