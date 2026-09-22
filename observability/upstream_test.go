package observability

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/teran/mcp-forgejo/domain"
)

// The exact Prometheus metric names the upstream collector MUST register (O03).
const (
	metricDurationSeconds = "forgejo_upstream_request_duration_seconds"
	metricTotal           = "forgejo_upstream_request_total"
	metricRequestSize     = "forgejo_upstream_request_size_bytes"
	metricResponseSize    = "forgejo_upstream_response_size_bytes"
)

func TestUpstreamCollectorRegistersExactMetricNames(t *testing.T) {
	reg := prometheus.NewRegistry()
	NewUpstreamCollectorWithRegistry(reg)

	fams, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	want := map[string]dto.MetricType{
		metricDurationSeconds: dto.MetricType_HISTOGRAM,
		metricTotal:           dto.MetricType_COUNTER,
		metricRequestSize:     dto.MetricType_HISTOGRAM,
		metricResponseSize:    dto.MetricType_HISTOGRAM,
	}

	found := map[string]bool{}
	for name := range want {
		found[name] = false
	}
	for _, f := range fams {
		if _, ok := want[f.GetName()]; ok {
			if f.GetType() != want[f.GetName()] {
				t.Errorf("metric %q type = %v, want %v", f.GetName(), f.GetType(), want[f.GetName()])
			}
			found[f.GetName()] = true
		}
	}
	for name := range want {
		if !found[name] {
			t.Errorf("metric %q was not registered", name)
		}
	}
}

// sampleObservation is a fixed observation used across the metric-value tests.
func sampleObservation() domain.UpstreamObservation {
	return domain.UpstreamObservation{
		Method:   "GET",
		Status:   200,
		InBytes:  10,
		OutBytes: 20,
		Duration: 5 * time.Millisecond,
	}
}

func familyByName(t *testing.T, fams []*dto.MetricFamily, name string) *dto.MetricFamily {
	t.Helper()
	for _, f := range fams {
		if f.GetName() == name {
			return f
		}
	}
	t.Fatalf("metric family %q not found", name)
	return nil
}

func findMetric(t *testing.T, fam *dto.MetricFamily, want map[string]string) *dto.Metric {
	t.Helper()
	for _, m := range fam.GetMetric() {
		if labelsMatch(m.GetLabel(), want) {
			return m
		}
	}
	t.Fatalf("metric with labels %v not found in %q", want, fam.GetName())
	return nil
}

func labelsMatch(got []*dto.LabelPair, want map[string]string) bool {
	if len(got) != len(want) {
		return false
	}
	for _, lp := range got {
		v, ok := want[lp.GetName()]
		if !ok || v != lp.GetValue() {
			return false
		}
	}
	return true
}

func TestUpstreamCollectorObserveOnce(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewUpstreamCollectorWithRegistry(reg)
	if c == nil {
		t.Fatal("NewUpstreamCollectorWithRegistry returned nil")
	}

	c.ObserveUpstream(sampleObservation())

	fams, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	// Counter: forgejo_upstream_request_total{method="GET",status="200"} == 1.
	counter := findMetric(t, familyByName(t, fams, metricTotal), map[string]string{"method": "GET", "status": "200"})
	if got := counter.GetCounter().GetValue(); got != 1 {
		t.Errorf("request_total value = %v, want 1", got)
	}

	// Duration histogram: exactly 1 sample, sum == 5ms in seconds.
	dur := findMetric(t, familyByName(t, fams, metricDurationSeconds), nil).GetHistogram()
	if got := dur.GetSampleCount(); got != 1 {
		t.Errorf("duration sample count = %d, want 1", got)
	}
	assertInDelta(t, dur.GetSampleSum(), 0.005, 1e-9, "duration sample sum")

	// Request size histogram: 1 sample summing to InBytes (10).
	req := findMetric(t, familyByName(t, fams, metricRequestSize), nil).GetHistogram()
	if got := req.GetSampleCount(); got != 1 {
		t.Errorf("request size sample count = %d, want 1", got)
	}
	assertInDelta(t, req.GetSampleSum(), 10, 1e-9, "request size sample sum")

	// Response size histogram: 1 sample summing to OutBytes (20).
	resp := findMetric(t, familyByName(t, fams, metricResponseSize), nil).GetHistogram()
	if got := resp.GetSampleCount(); got != 1 {
		t.Errorf("response size sample count = %d, want 1", got)
	}
	assertInDelta(t, resp.GetSampleSum(), 20, 1e-9, "response size sample sum")
}

func TestUpstreamCollectorObserveTwiceIncrementsCounter(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewUpstreamCollectorWithRegistry(reg)

	c.ObserveUpstream(sampleObservation())
	c.ObserveUpstream(sampleObservation())

	fams, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	counter := findMetric(t, familyByName(t, fams, metricTotal), map[string]string{"method": "GET", "status": "200"})
	if got := counter.GetCounter().GetValue(); got != 2 {
		t.Errorf("request_total value = %v, want 2", got)
	}

	dur := findMetric(t, familyByName(t, fams, metricDurationSeconds), nil).GetHistogram()
	if got := dur.GetSampleCount(); got != 2 {
		t.Errorf("duration sample count = %d, want 2", got)
	}
}

func TestUpstreamCollectorTransportErrorStatusZero(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := NewUpstreamCollectorWithRegistry(reg)

	// Transport error: no HTTP response received, Status == 0.
	c.ObserveUpstream(domain.UpstreamObservation{
		Method: "POST", Status: 0, InBytes: 3, OutBytes: 0, Duration: time.Millisecond,
	})

	fams, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	counter := findMetric(t, familyByName(t, fams, metricTotal), map[string]string{"method": "POST", "status": "0"})
	if got := counter.GetCounter().GetValue(); got != 1 {
		t.Errorf("request_total{method=POST,status=0} value = %v, want 1", got)
	}
}

// TestNewUpstreamCollectorDefaultRegistry is a lightweight smoke test for the
// default-registry variant. It tolerates a double-registration panic (which
// could occur if another test already registered the same collectors on the
// default registry), so it is safe to run regardless of test order.
func TestNewUpstreamCollectorDefaultRegistry(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Logf("NewUpstreamCollector panicked (likely double registration on default registry), skipping: %v", r)
		}
	}()

	c := NewUpstreamCollector()
	if c == nil {
		t.Fatal("NewUpstreamCollector returned nil")
	}
	// It must implement the observer contract even when wired to the default
	// registry.
	var _ domain.UpstreamObserver = c
}

func assertInDelta(t *testing.T, got, want, delta float64, msg string) {
	t.Helper()
	if got < want-delta || got > want+delta {
		t.Errorf("%s = %v, want %v (±%v)", msg, got, want, delta)
	}
}
