package domain

import "time"

// UpstreamObservation describes a single outbound upstream (Forgejo) HTTP
// request after it has completed, so infrastructure can emit metrics without
// depending on the observability implementation (O03).
type UpstreamObservation struct {
	Method   string
	Status   int
	InBytes  int64
	OutBytes int64
	Duration time.Duration
}

// UpstreamObserver receives one observation per outbound upstream (Forgejo)
// HTTP request, so infrastructure can emit metrics without depending on the
// observability implementation (O03).
type UpstreamObserver interface {
	ObserveUpstream(r UpstreamObservation)
}
