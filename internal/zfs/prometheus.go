package zfs

import "github.com/prometheus/client_golang/prometheus"

var prom struct {
	ZFSListFilesystemVersionDuration          *prometheus.HistogramVec
	ZFSSnapshotDuration                       *prometheus.HistogramVec
	ZFSBookmarkDuration                       *prometheus.HistogramVec
	ZFSDestroyDuration                        *prometheus.HistogramVec
	ZFSListUnmatchedUserSpecifiedDatasetCount *prometheus.GaugeVec
}

func init() {
	prom.ZFSListFilesystemVersionDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "zrepl",
		Subsystem: "zfs",
		Name:      "list_filesystem_versions_duration",
		Help:      "Seconds it took for listing the versions of a given filesystem",
	}, []string{"filesystem"})
	prom.ZFSSnapshotDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "zrepl",
		Subsystem: "zfs",
		Name:      "snapshot_duration",
		Help:      "Seconds it took to create a snapshot a given filesystem",
	}, []string{"filesystem"})
	prom.ZFSBookmarkDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "zrepl",
		Subsystem: "zfs",
		Name:      "bookmark_duration",
		Help:      "Duration it took to bookmark a given snapshot",
	}, []string{"filesystem"})
	prom.ZFSDestroyDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "zrepl",
		Subsystem: "zfs",
		Name:      "destroy_duration",
		Help:      "Duration it took to destroy a dataset",
	}, []string{"dataset_type", "filesystem"})
	prom.ZFSListUnmatchedUserSpecifiedDatasetCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "zrepl",
		Subsystem: "zfs",
		Name:      "list_unmatched_user_specified_dataset_count",
		Help: "When evaluating a DatsetFilter against zfs list output, this counter " +
			"is incremented for every DatasetFilter rule that did not match any " +
			"filesystem name in the zfs list output. Monitor for increases to detect filesystem " +
			"filter rules that have no effect because they don't match any local filesystem.",
	}, []string{"jobid"})
}

func PrometheusRegister(registry prometheus.Registerer) error {
	if err := registry.Register(prom.ZFSListFilesystemVersionDuration); err != nil {
		return err
	}
	if err := registry.Register(prom.ZFSBookmarkDuration); err != nil {
		return err
	}
	if err := registry.Register(prom.ZFSSnapshotDuration); err != nil {
		return err
	}
	if err := registry.Register(prom.ZFSDestroyDuration); err != nil {
		return err
	}
	if err := registry.Register(prom.ZFSListUnmatchedUserSpecifiedDatasetCount); err != nil {
		return err
	}
	return nil
}
