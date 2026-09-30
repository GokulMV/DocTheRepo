package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds every Prometheus collector the Hub exports (plan § 11). Later phases add their
// collectors here so the full metric surface is visible in one place.
type Metrics struct {
	Registry *prometheus.Registry

	HTTPRequests *prometheus.CounterVec
	HTTPDuration *prometheus.HistogramVec

	JobsTotal    *prometheus.CounterVec
	JobDuration  *prometheus.HistogramVec
	QueueDepth   *prometheus.GaugeVec
	JobsInFlight *prometheus.GaugeVec
	// Retrieval times each Q&A retrieval stage (embed, vector_search, full_text, load_expand, total).
	Retrieval *prometheus.HistogramVec
}

// NewMetrics registers all collectors on a fresh registry (never the global one, so tests stay isolated).
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m := &Metrics{
		Registry: reg,
		HTTPRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dth_http_requests_total", Help: "HTTP requests by route pattern and status code.",
		}, []string{"route", "code"}),
		HTTPDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "dth_http_duration_seconds", Help: "HTTP request latency by route pattern.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route"}),
		JobsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "dth_jobs_total", Help: "Jobs finished by type and final status (including retries scheduled).",
		}, []string{"type", "status"}),
		JobDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "dth_job_duration_seconds", Help: "Job handler run time by type.",
			Buckets: []float64{0.1, 0.5, 1, 5, 15, 30, 60, 120, 300, 600, 1200},
		}, []string{"type"}),
		QueueDepth: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "dth_queue_depth", Help: "Queued jobs by type.",
		}, []string{"type"}),
		JobsInFlight: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "dth_jobs_in_flight", Help: "Jobs currently being processed by this replica, by type.",
		}, []string{"type"}),
		Retrieval: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "dth_retrieval_duration_seconds", Help: "Q&A retrieval latency by stage.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.2, 0.3, 0.4, 0.5, 0.75, 1, 2, 5},
		}, []string{"stage"}),
	}
	reg.MustRegister(m.HTTPRequests, m.HTTPDuration, m.JobsTotal, m.JobDuration, m.QueueDepth, m.JobsInFlight, m.Retrieval)
	return m
}

// Handler serves the Prometheus text exposition for this registry.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{Registry: m.Registry})
}
