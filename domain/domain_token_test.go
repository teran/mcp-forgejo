package domain

import (
	"context"
	"testing"
)

// TestWithTokenRoundTrip verifies that a token stored via WithToken is returned
// unchanged by TokenFromContext.
func TestWithTokenRoundTrip(t *testing.T) {
	ctx := WithToken(context.Background(), "tok-abc123")
	tok, ok := TokenFromContext(ctx)
	if !ok {
		t.Fatal("expected token to be present")
	}
	if tok != "tok-abc123" {
		t.Errorf("TokenFromContext() = %q, want %q", tok, "tok-abc123")
	}
}

// TestTokenFromContextAbsent verifies the absent case returns empty and false.
func TestTokenFromContextAbsent(t *testing.T) {
	tok, ok := TokenFromContext(context.Background())
	if ok {
		t.Errorf("TokenFromContext() ok = true, want false (tok=%q)", tok)
	}
	if tok != "" {
		t.Errorf("TokenFromContext() tok = %q, want empty", tok)
	}
}

// TestTokenFromContextTypeSafety verifies that an empty-but-present token still
// reports ok=true, mirroring the request-ID helpers (callers rely on the
// presence to distinguish "no token" from "empty token").
func TestTokenFromContextTypeSafety(t *testing.T) {
	ctx := WithToken(context.Background(), "")
	tok, ok := TokenFromContext(ctx)
	if !ok {
		t.Fatal("expected empty token to be present")
	}
	if tok != "" {
		t.Errorf("tok = %q, want empty", tok)
	}
}

// TestWithTokenIsNewContext verifies WithToken never mutates its input context
// (returns a derived context).
func TestWithTokenIsNewContext(t *testing.T) {
	base := context.Background()
	_ = WithToken(base, "x")
	if _, ok := TokenFromContext(base); ok {
		t.Error("WithToken mutated the input context")
	}
}

// TestWithTokenInherits verifies a token set on a parent context is visible
// from a derived child context (survives context propagation).
func TestWithTokenInherits(t *testing.T) {
	type unrelatedKey struct{}
	parent := WithToken(context.Background(), "parent-tok")
	child := context.WithValue(parent, unrelatedKey{}, "unrelated")

	if tok, ok := TokenFromContext(child); !ok || tok != "parent-tok" {
		t.Errorf("token not inherited: tok=%q ok=%v", tok, ok)
	}
}
