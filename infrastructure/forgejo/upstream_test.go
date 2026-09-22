package forgejo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/teran/mcp-forgejo/domain"
)

// recordingObserver implements domain.UpstreamObserver and records every
// observation for later assertion. A mutex guards the slice so the fake is safe
// even if the client were to observe from multiple goroutines.
type recordingObserver struct {
	mu  sync.Mutex
	got []domain.UpstreamObservation
}

func (r *recordingObserver) ObserveUpstream(o domain.UpstreamObservation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, o)
}

func (r *recordingObserver) observations() []domain.UpstreamObservation {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.UpstreamObservation, len(r.got))
	copy(out, r.got)
	return out
}

// Compile-time assertion that the recording fake satisfies the interface.
var _ domain.UpstreamObserver = (*recordingObserver)(nil)

const upstreamRepoJSON = `{"id":7,"name":"demo","full_name":"acme/demo","private":true,"default_branch":"master","description":"a repo"}`

func TestObserveUpstreamHappyPath(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(upstreamRepoJSON))
	}))
	t.Cleanup(ts.Close)

	c := NewWithClient(Config{BaseURL: ts.URL, Token: testToken}, ts.Client())
	obs := &recordingObserver{}
	c.SetObserver(obs)

	if _, err := c.GetRepository(context.Background(), "acme", "demo"); err != nil {
		t.Fatalf("GetRepository() error = %v", err)
	}

	got := obs.observations()
	if len(got) != 1 {
		t.Fatalf("observed %d observations, want exactly 1", len(got))
	}
	o := got[0]
	if o.Method != http.MethodGet {
		t.Errorf("Method = %q, want GET", o.Method)
	}
	if o.Status != http.StatusOK {
		t.Errorf("Status = %d, want 200", o.Status)
	}
	if o.OutBytes != int64(len(upstreamRepoJSON)) {
		t.Errorf("OutBytes = %d, want %d", o.OutBytes, len(upstreamRepoJSON))
	}
	if o.InBytes < 0 {
		t.Errorf("InBytes = %d, want >= 0", o.InBytes)
	}
	if o.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", o.Duration)
	}
}

func TestObserveUpstreamNon2xx(t *testing.T) {
	const notFoundBody = `{"message":"not found"}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(notFoundBody))
	}))
	t.Cleanup(ts.Close)

	c := NewWithClient(Config{BaseURL: ts.URL, Token: testToken}, ts.Client())
	obs := &recordingObserver{}
	c.SetObserver(obs)

	if _, err := c.GetRepository(context.Background(), "acme", "missing"); err == nil {
		t.Fatal("GetRepository() returned nil error for a 404 response")
	}

	got := obs.observations()
	if len(got) != 1 {
		t.Fatalf("observed %d observations, want exactly 1", len(got))
	}
	o := got[0]
	if o.Method != http.MethodGet {
		t.Errorf("Method = %q, want GET", o.Method)
	}
	if o.Status != http.StatusNotFound {
		t.Errorf("Status = %d, want 404", o.Status)
	}
	if o.OutBytes != int64(len(notFoundBody)) {
		t.Errorf("OutBytes = %d, want %d", o.OutBytes, len(notFoundBody))
	}
}

func TestNoObserverDoesNotPanic(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(upstreamRepoJSON))
	}))
	t.Cleanup(ts.Close)

	c := NewWithClient(Config{BaseURL: ts.URL, Token: testToken}, ts.Client())
	// No SetObserver call: the default observer must be nil and a no-op.

	if _, err := c.GetRepository(context.Background(), "acme", "demo"); err != nil {
		t.Fatalf("GetRepository() with no observer error = %v", err)
	}
}

func TestSetObserverNilIsNoOp(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(upstreamRepoJSON))
	}))
	t.Cleanup(ts.Close)

	c := NewWithClient(Config{BaseURL: ts.URL, Token: testToken}, ts.Client())
	c.SetObserver(nil) // must not panic and must not observe

	if _, err := c.GetRepository(context.Background(), "acme", "demo"); err != nil {
		t.Fatalf("GetRepository() with nil observer error = %v", err)
	}
}
