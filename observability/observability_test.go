package observability

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// newTestLogger returns a logrus.Logger whose output is discarded so tests do
// not write to the test process stdout/stderr.
func newTestLogger() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(io.Discard)
	return l
}

func TestNewHandlerServesMetrics(t *testing.T) {
	h := NewHandler()

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /metrics status = %d, want 200", rr.Code)
	}

	body := rr.Body.String()
	for _, want := range []string{
		"go_goroutines",
		"go_gc_duration_seconds",
		"promhttp_metric_handler_requests_total",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /metrics body does not contain %q", want)
		}
	}
}

func TestNewHandlerServesHealthz(t *testing.T) {
	h := NewHandler()

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want 200", rr.Code)
	}
}

func TestNewHandlerServesReadyz(t *testing.T) {
	h := NewHandler()

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /readyz status = %d, want 200", rr.Code)
	}
}

func TestNewHandlerServesPprof(t *testing.T) {
	h := NewHandler()

	for _, path := range []string{"/debug/pprof/", "/debug/pprof/cmdline"} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", path, rr.Code)
		}
	}
}

// freePort reserves an ephemeral localhost TCP port and returns its address
// string. The listener is closed before returning, so the returned address is
// free for the server under test to bind.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().String()
}

// waitForHealthz polls the given address until GET /healthz returns 200 or the
// deadline passes, returning the last status code seen (0 if never reached).
func waitForHealthz(addr string) int {
	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)
	last := 0
	for time.Now().Before(deadline) {
		resp, err := client.Get("http://" + addr + "/healthz")
		if err == nil {
			last = resp.StatusCode
			_ = resp.Body.Close()
			if last == http.StatusOK {
				return last
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return last
}

func TestRunServesAndShutsDownGracefully(t *testing.T) {
	addr := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, addr, newTestLogger()) }()

	if got := waitForHealthz(addr); got != http.StatusOK {
		t.Fatalf("GET /healthz never returned 200, last = %d", got)
	}

	// While running, the observability server must also expose metrics.
	resp, err := http.Get("http://" + addr + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics on running server: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /metrics status = %d, want 200", resp.StatusCode)
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run returned error after graceful shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

// TestRunWithNilLogger verifies Run works when no logger is provided (the
// optional logging path must not be exercised with a nil receiver).
func TestRunWithNilLogger(t *testing.T) {
	addr := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, addr, nil) }()

	if got := waitForHealthz(addr); got != http.StatusOK {
		t.Fatalf("GET /healthz never returned 200, last = %d", got)
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run returned error after graceful shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

// TestRunDoesNotLogFailureOnGracefulShutdown verifies that a normal shutdown
// (context cancellation) does NOT log an "observability server failed" error:
// the ListenAndServe ErrServerClosed path must be treated as expected, not as
// a failure.
func TestRunDoesNotLogFailureOnGracefulShutdown(t *testing.T) {
	addr := freePort(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var buf bytes.Buffer
	log := logrus.New()
	log.SetOutput(&buf)

	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, addr, log) }()

	if got := waitForHealthz(addr); got != http.StatusOK {
		t.Fatalf("GET /healthz never returned 200, last = %d", got)
	}

	cancel()

	if err := <-errCh; err != nil {
		t.Fatalf("Run returned error after graceful shutdown: %v", err)
	}

	// The shutdown and any error logging happen asynchronously after Run
	// returns, so poll briefly to let the ListenAndServe goroutine settle.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), "observability server failed") {
			t.Fatalf("graceful shutdown logged a server failure: %s", buf.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
