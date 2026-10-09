package controller

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Metrics carries only counts and timestamps; no node, namespace or workload name is ever
// a label value. The only label that varies is the name of the CgroupReadiness resource.
type Metrics struct {
	NodesV1     *prometheus.GaugeVec
	NodesByMode *prometheus.GaugeVec
	Findings    *prometheus.GaugeVec
	LastScan    *prometheus.GaugeVec
	Duration    prometheus.Histogram
	Failures    *prometheus.CounterVec
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		NodesV1:     prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "cgroupguard_nodes_cgroup_v1", Help: "Linux nodes measured on cgroup v1."}, []string{"name"}),
		NodesByMode: prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "cgroupguard_nodes", Help: "Linux nodes by measured cgroup mode (v1, v2, hybrid, unknown)."}, []string{"name", "cgroup"}),
		Findings:    prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "cgroupguard_findings", Help: "Findings by severity from the last successful scan."}, []string{"name", "severity"}),
		LastScan:    prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "cgroupguard_last_scan_timestamp_seconds", Help: "Unix time of the last successful scan."}, []string{"name"}),
		Duration:    prometheus.NewHistogram(prometheus.HistogramOpts{Name: "cgroupguard_scan_duration_seconds", Help: "Duration of scans.", Buckets: prometheus.ExponentialBuckets(0.1, 2, 10)}),
		Failures:    prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cgroupguard_scan_failures_total", Help: "Scans that failed."}, []string{"name"}),
	}
	reg.MustRegister(m.NodesV1, m.NodesByMode, m.Findings, m.LastScan, m.Duration, m.Failures)
	return m
}

// Observe records a successful scan.
func (m *Metrics) Observe(name string, c Counts, unix float64, seconds float64) {
	m.NodesV1.WithLabelValues(name).Set(float64(c.V1))
	for mode, n := range map[string]int{"v1": c.V1, "v2": c.V2, "hybrid": c.Hybrid, "unknown": c.Unknown} {
		m.NodesByMode.WithLabelValues(name, mode).Set(float64(n))
	}
	m.Findings.WithLabelValues(name, "high").Set(float64(c.High))
	m.Findings.WithLabelValues(name, "warning").Set(float64(c.Warning))
	m.Findings.WithLabelValues(name, "note").Set(float64(c.Note))
	m.LastScan.WithLabelValues(name).Set(unix)
	m.Duration.Observe(seconds)
}

// Forget removes the series of a deleted resource.
func (m *Metrics) Forget(name string) {
	m.NodesV1.DeleteLabelValues(name)
	for _, mode := range []string{"v1", "v2", "hybrid", "unknown"} {
		m.NodesByMode.DeleteLabelValues(name, mode)
	}
	for _, sev := range []string{"high", "warning", "note"} {
		m.Findings.DeleteLabelValues(name, sev)
	}
	m.LastScan.DeleteLabelValues(name)
	m.Failures.DeleteLabelValues(name)
}

// DefaultRegistry is the controller-runtime registry served on the metrics port.
func DefaultRegistry() prometheus.Registerer { return ctrlmetrics.Registry }
