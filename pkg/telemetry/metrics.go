package telemetry

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics interface for collecting and exposing metrics.
type Metrics interface {
	IncRequests(component, method, tool, code string)
	ObserveLatency(component, method string, durationMs float64)
	IncErrors(component, code string)
	Handler() http.Handler
}

// prometheusMetrics implements Metrics using Prometheus.
type prometheusMetrics struct {
	registry      *prometheus.Registry
	requestsTotal *prometheus.CounterVec
	latency       *prometheus.HistogramVec
	errorsTotal   *prometheus.CounterVec
	handler       http.Handler
}

// NewMetrics creates a new Prometheus metrics collector with default registry.
func NewMetrics() Metrics {
	return NewMetricsWithRegistry(prometheus.DefaultRegisterer.(*prometheus.Registry))
}

// NewMetricsWithRegistry creates a new Prometheus metrics collector with custom registry.
func NewMetricsWithRegistry(reg *prometheus.Registry) Metrics {
	m := &prometheusMetrics{
		registry: reg,
	}

	// Requests counter: component, method, tool, code
	m.requestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "molu_requests_total",
			Help: "Total number of MCP requests processed",
		},
		[]string{"component", "method", "tool", "code"},
	)

	// Latency histogram: component, method
	m.latency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "molu_latency_seconds",
			Help:    "Latency of MCP requests in seconds",
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"component", "method"},
	)

	// Errors counter: component, code
	m.errorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "molu_errors_total",
			Help: "Total number of errors by type",
		},
		[]string{"component", "code"},
	)

	// Register all collectors
	m.registry.MustRegister(m.requestsTotal, m.latency, m.errorsTotal)

	// HTTP handler for /metrics
	m.handler = promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})

	return m
}

func (m *prometheusMetrics) IncRequests(component, method, tool, code string) {
	// Empty tool for non-tool requests (e.g., admit/score)
	if tool == "" {
		tool = "-"
	}
	m.requestsTotal.WithLabelValues(component, method, tool, code).Inc()
}

func (m *prometheusMetrics) ObserveLatency(component, method string, durationMs float64) {
	// Convert ms to seconds
	m.latency.WithLabelValues(component, method).Observe(durationMs / 1000.0)
}

func (m *prometheusMetrics) IncErrors(component, code string) {
	m.errorsTotal.WithLabelValues(component, code).Inc()
}

func (m *prometheusMetrics) Handler() http.Handler {
	return m.handler
}