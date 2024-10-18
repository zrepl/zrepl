package frameconn

import "github.com/prometheus/client_golang/prometheus"

var prom struct {
	ShutdownDrainBytesRead prometheus.Summary
	ShutdownSeconds        prometheus.Summary
	ShutdownDrainSeconds   prometheus.Summary
	ShutdownHardCloses     *prometheus.CounterVec
	ShutdownCloseErrors    *prometheus.CounterVec
}

func init() {
	prom.ShutdownDrainBytesRead = prometheus.NewSummary(prometheus.SummaryOpts{
		Namespace: "zrepl",
		Subsystem: "frameconn",
		Name:      "shutdown_drain_bytes_read",
		Help:      "Number of bytes read during the drain phase of connection shutdown",
	})
	prom.ShutdownSeconds = prometheus.NewSummary(prometheus.SummaryOpts{
		Namespace: "zrepl",
		Subsystem: "frameconn",
		Name:      "shutdown_seconds",
		Help:      "Seconds it took for connection shutdown to complete",
	})
	prom.ShutdownDrainSeconds = prometheus.NewSummary(prometheus.SummaryOpts{
		Namespace: "zrepl",
		Subsystem: "frameconn",
		Name:      "shutdown_drain_seconds",
		Help:      "Seconds it took from read-side-drain until shutdown completion",
	})
	prom.ShutdownHardCloses = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "zrepl",
		Subsystem: "frameconn",
		Name:      "shutdown_hard_closes",
		Help:      "Number of hard connection closes during shutdown (abortive close)",
	}, []string{"step"})
	prom.ShutdownCloseErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "zrepl",
		Subsystem: "frameconn",
		Name:      "shutdown_close_errors",
		Help:      "Number of errors closing the underlying network connection. Should alert on this",
	}, []string{"step"})
}

func PrometheusRegister(registry prometheus.Registerer) error {
	if err := registry.Register(prom.ShutdownDrainBytesRead); err != nil {
		return err
	}
	if err := registry.Register(prom.ShutdownSeconds); err != nil {
		return err
	}
	if err := registry.Register(prom.ShutdownDrainSeconds); err != nil {
		return err
	}
	if err := registry.Register(prom.ShutdownHardCloses); err != nil {
		return err
	}
	if err := registry.Register(prom.ShutdownCloseErrors); err != nil {
		return err
	}
	return nil
}
