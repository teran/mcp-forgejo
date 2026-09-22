package domain

import (
	"testing"
	"time"
)

// recordingUpstreamObserver is a concrete implementation of UpstreamObserver
// used to pin the compile-time contract (O03): infrastructure can attach any
// observer that satisfies the interface without depending on the observability
// implementation.
type recordingUpstreamObserver struct {
	got []UpstreamObservation
}

func (r *recordingUpstreamObserver) ObserveUpstream(o UpstreamObservation) {
	r.got = append(r.got, o)
}

// Compile-time assertion: the concrete type satisfies the interface. If the
// developer changes the interface signature, this line stops compiling.
var _ UpstreamObserver = (*recordingUpstreamObserver)(nil)

func TestUpstreamObservationFields(t *testing.T) {
	o := UpstreamObservation{
		Method:   "GET",
		Status:   200,
		InBytes:  10,
		OutBytes: 20,
		Duration: 5 * time.Millisecond,
	}

	if o.Method != "GET" {
		t.Errorf("Method = %q, want %q", o.Method, "GET")
	}
	if o.Status != 200 {
		t.Errorf("Status = %d, want 200", o.Status)
	}
	if o.InBytes != 10 {
		t.Errorf("InBytes = %d, want 10", o.InBytes)
	}
	if o.OutBytes != 20 {
		t.Errorf("OutBytes = %d, want 20", o.OutBytes)
	}
	if o.Duration != 5*time.Millisecond {
		t.Errorf("Duration = %v, want 5ms", o.Duration)
	}
}

func TestUpstreamObservationZeroValueIsTransportError(t *testing.T) {
	// The zero value represents a transport error where no HTTP response was
	// received: Status must be 0 so the metrics layer labels it "0".
	var o UpstreamObservation
	if o.Status != 0 {
		t.Errorf("zero Status = %d, want 0", o.Status)
	}
	if o.Method != "" {
		t.Errorf("zero Method = %q, want empty", o.Method)
	}
	if o.InBytes != 0 || o.OutBytes != 0 {
		t.Errorf("zero byte counts = %d/%d, want 0/0", o.InBytes, o.OutBytes)
	}
}

func TestUpstreamObserverInterfaceDispatch(t *testing.T) {
	var obs UpstreamObserver = &recordingUpstreamObserver{}
	obs.ObserveUpstream(UpstreamObservation{
		Method: "GET", Status: 200, InBytes: 1, OutBytes: 2, Duration: time.Millisecond,
	})

	rec := obs.(*recordingUpstreamObserver)
	if len(rec.got) != 1 {
		t.Fatalf("observed %d observations, want 1", len(rec.got))
	}
	got := rec.got[0]
	if got.Method != "GET" || got.Status != 200 || got.InBytes != 1 || got.OutBytes != 2 || got.Duration != time.Millisecond {
		t.Errorf("unexpected observation: %+v", got)
	}
}
