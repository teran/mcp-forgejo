package domain

import (
	"testing"
)

func TestRedact(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		secret string
		want   string
	}{
		{"empty secret", "hello world", "", "hello world"},
		{"no occurrence", "the quick brown fox", "secret", "the quick brown fox"},
		{"single occurrence", "token abc123 and more", "abc123", "token [REDACTED] and more"},
		{"multiple occurrences", "a abc123 b abc123", "abc123", "a [REDACTED] b [REDACTED]"},
		{"empty source", "", "abc123", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Redact(tt.s, tt.secret); got != tt.want {
				t.Errorf("Redact() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestForgejoError(t *testing.T) {
	e := &ForgejoError{Kind: KindNotFound, Message: "Forgejo resource not found"}
	if got := e.Error(); got != "Forgejo resource not found" {
		t.Errorf("Error() = %q, want %q", got, "Forgejo resource not found")
	}
	if e.Kind != KindNotFound {
		t.Errorf("Kind = %q, want %q", e.Kind, KindNotFound)
	}
}

func TestNewForgejoError(t *testing.T) {
	err := NewForgejoError(KindConflict, "conflict occurred")
	fe, ok := err.(*ForgejoError)
	if !ok {
		t.Fatalf("expected *ForgejoError, got %T", err)
	}
	if fe.Kind != KindConflict {
		t.Errorf("Kind = %q, want %q", fe.Kind, KindConflict)
	}
	if fe.Message != "conflict occurred" {
		t.Errorf("Message = %q, want %q", fe.Message, "conflict occurred")
	}
}
