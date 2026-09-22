package server

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/teran/mcp-forgejo/domain"
	"github.com/teran/mcp-forgejo/infrastructure/forgejo"
)

// recordingUpstreamObserver implements domain.UpstreamObserver and records
// every observation for later assertion.
type recordingUpstreamObserver struct {
	mu  sync.Mutex
	got []domain.UpstreamObservation
}

func (r *recordingUpstreamObserver) ObserveUpstream(o domain.UpstreamObservation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, o)
}

func (r *recordingUpstreamObserver) observations() []domain.UpstreamObservation {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.UpstreamObservation, len(r.got))
	copy(out, r.got)
	return out
}

// Compile-time assertion that the recording fake satisfies the interface.
var _ domain.UpstreamObserver = (*recordingUpstreamObserver)(nil)

func TestBuildWithObserverAttachesObserver(t *testing.T) {
	ts := httptest.NewServer(mockForgejo())
	t.Cleanup(ts.Close)

	obs := &recordingUpstreamObserver{}
	s, err := BuildWithObserver(forgejo.Config{BaseURL: ts.URL, Token: testToken}, nil, obs)
	if err != nil {
		t.Fatalf("BuildWithObserver() error = %v", err)
	}
	if s == nil {
		t.Fatal("BuildWithObserver() returned nil server")
	}

	// Drive a real tool call so the client makes an outbound request to Forgejo,
	// which must flow through the attached observer.
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := s.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	var out domain.Repository
	callTool(t, cs, "forgejo_repo_get", map[string]any{"owner": "acme", "repo": "demo"}, &out)

	got := obs.observations()
	if len(got) == 0 {
		t.Fatal("observer received no observations after a tool call")
	}
	o := got[0]
	if o.Method != "GET" {
		t.Errorf("Method = %q, want GET", o.Method)
	}
	if o.Status != 200 {
		t.Errorf("Status = %d, want 200", o.Status)
	}
}

func TestBuildWithObserverNilObserverDoesNotPanic(t *testing.T) {
	ts := httptest.NewServer(mockForgejo())
	t.Cleanup(ts.Close)

	s, err := BuildWithObserver(forgejo.Config{BaseURL: ts.URL, Token: testToken}, nil, nil)
	if err != nil {
		t.Fatalf("BuildWithObserver(nil observer) error = %v", err)
	}
	if s == nil {
		t.Fatal("BuildWithObserver(nil observer) returned nil server")
	}
}

func TestBuildWithLoggerStillWorksWithoutObserver(t *testing.T) {
	ts := httptest.NewServer(mockForgejo())
	t.Cleanup(ts.Close)

	s, err := BuildWithLogger(forgejo.Config{BaseURL: ts.URL, Token: testToken}, nil)
	if err != nil {
		t.Fatalf("BuildWithLogger() error = %v", err)
	}
	if s == nil {
		t.Fatal("BuildWithLogger() returned nil server")
	}
}

func TestBuildStillWorks(t *testing.T) {
	ts := httptest.NewServer(mockForgejo())
	t.Cleanup(ts.Close)

	s, err := Build(forgejo.Config{BaseURL: ts.URL, Token: testToken})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if s == nil {
		t.Fatal("Build() returned nil server")
	}
}
