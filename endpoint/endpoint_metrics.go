package endpoint

import "github.com/prometheus/client_golang/prometheus"

func RegisterMetrics(r prometheus.Registerer) {
	r.MustRegister(sendAbstractionsCacheMetrics.count)
}
