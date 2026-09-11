package domain

import (
	"encoding/json"
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

// The following tests pin the JSON shape of the NEW Batch A domain types that
// @developer must add to domain.go. They act as test expectations: the types
// (and their JSON tags) are specified here first so the implementation matches
// exactly. Round-trip (marshal+unmarshal) exercises the tags so a mutated or
// dropped tag is caught.

func TestCommitJSONRoundTrip(t *testing.T) {
	in := Commit{SHA: "abc123", Message: "fix: build", Author: "Alice", Date: "2024-01-01T00:00:00Z"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Commit
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Errorf("round trip mismatch: %+v != %+v", out, in)
	}
}

func TestBranchJSONRoundTrip(t *testing.T) {
	in := Branch{Name: "main", Protected: true, Default: true, CommitSHA: "sha1"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Branch
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Errorf("round trip mismatch: %+v != %+v", out, in)
	}
}

func TestDiffJSONRoundTrip(t *testing.T) {
	in := Diff{BaseHead: "main..dev", Text: "@@ -1 +1 @@\n-old\n+new\n"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Diff
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Errorf("round trip mismatch: %+v != %+v", out, in)
	}
}

func TestPullRequestJSONRoundTrip(t *testing.T) {
	in := PullRequest{ID: 1, Number: 5, Title: "t", Body: "b", State: "open", User: User{Login: "alice"}, HTMLURL: "https://x/5"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out PullRequest
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Errorf("round trip mismatch: %+v != %+v", out, in)
	}
}

func TestPullFileJSONRoundTrip(t *testing.T) {
	in := PullFile{Filename: "a.go", Status: "modified", Additions: 1, Deletions: 2, Changes: 3}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out PullFile
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Errorf("round trip mismatch: %+v != %+v", out, in)
	}
}

func TestCheckJSONRoundTrip(t *testing.T) {
	in := Check{Context: "ci", State: "success", TargetURL: "https://ci/x", Description: "ok"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Check
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Errorf("round trip mismatch: %+v != %+v", out, in)
	}
}

func TestPullRequestDetailJSONRoundTrip(t *testing.T) {
	in := PullRequestDetail{
		PullRequest: PullRequest{Number: 5, Title: "t"},
		Files:       []PullFile{{Filename: "a.go", Status: "modified"}},
		Checks:      []Check{{Context: "ci", State: "success"}},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out PullRequestDetail
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Files) != 1 || len(out.Checks) != 1 || out.PullRequest.Number != 5 {
		t.Errorf("round trip mismatch: %+v", out)
	}
}

func TestReleaseJSONRoundTrip(t *testing.T) {
	in := Release{ID: 1, TagName: "v1", Name: "V1", Body: "notes", Draft: false, Prerelease: true, CreatedAt: "2024-01-01"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Release
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Errorf("round trip mismatch: %+v != %+v", out, in)
	}
}
