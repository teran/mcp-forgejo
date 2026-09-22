package observability

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/teran/mcp-forgejo/domain"
)

// Upstream metric names (O03). These exact names/types are load-bearing: the
// conformance tests assert them, so they must not be renamed.
const (
	upstreamDurationSeconds = "forgejo_upstream_request_duration_seconds"
	upstreamTotal           = "forgejo_upstream_request_total"
	upstreamRequestSize     = "forgejo_upstream_request_size_bytes"
	upstreamResponseSize    = "forgejo_upstream_response_size_bytes"
)

// UpstreamCollector exposes per-outbound-request Forgejo metrics (O03) and
// implements domain.UpstreamObserver. It is concurrency-safe: prometheus
// histograms and counter vectors are safe for concurrent use.
type UpstreamCollector struct {
	duration prometheus.Histogram
	total    *prometheus.CounterVec
	reqSize  prometheus.Histogram
	respSize prometheus.Histogram
}

// NewUpstreamCollector registers the upstream Forgejo metrics on the DEFAULT
// registry (the one /metrics uses) and returns the observer.
func NewUpstreamCollector() *UpstreamCollector {
	return NewUpstreamCollectorWithRegistry(prometheus.DefaultRegisterer)
}

// NewUpstreamCollectorWithRegistry registers the upstream Forgejo metrics on an
// explicit Registerer (for tests).
func NewUpstreamCollectorWithRegistry(reg prometheus.Registerer) *UpstreamCollector {
	c := &UpstreamCollector{
		duration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: upstreamDurationSeconds,
			Help: "Duration of outbound Forgejo HTTP requests in seconds.",
		}),
		total: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: upstreamTotal,
			Help: "Total number of outbound Forgejo HTTP requests by method and HTTP status (0 on transport error).",
		}, []string{"method", "status"}),
		reqSize: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: upstreamRequestSize,
			Help: "Size of outbound Forgejo request bodies in bytes.",
		}),
		respSize: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: upstreamResponseSize,
			Help: "Size of outbound Forgejo response bodies in bytes.",
		}),
	}
	// Seed the counter with an empty label combination so its metric family is
	// always present on Gather — including before the first request, which the
	// registration tests assert and which is useful for alerting on zero
	// traffic. A CounterVec with no children would otherwise emit no family.
	c.total.WithLabelValues("", "")
	reg.MustRegister(c.duration, c.total, c.reqSize, c.respSize)
	return c
}

// ObserveUpstream implements domain.UpstreamObserver, recording one observation
// per completed outbound request.
func (c *UpstreamCollector) ObserveUpstream(r domain.UpstreamObservation) {
	c.total.WithLabelValues(r.Method, strconv.Itoa(r.Status)).Inc()
	c.duration.Observe(r.Duration.Seconds())
	c.reqSize.Observe(float64(r.InBytes))
	c.respSize.Observe(float64(r.OutBytes))
}
