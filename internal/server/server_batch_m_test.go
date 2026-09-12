package server

// Batch M tools (SPEC §6): forgejo_milestone_list/create/update/delete,
// forgejo_label_list/create/update/delete, forgejo_issue_set_labels. These
// tests verify the tools are registered, carry the correct annotations
// (milestone_list/label_list read, milestone_create/label_create/issue_set_labels
// write non-idempotent, milestone_update/label_update write idempotent,
// milestone_delete/label_delete destructive) and are invocable end to end
// against a mock Forgejo. The input field names below are the JSON schema
// fields @developer must expose on the server input structs.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"example.com/teran/mcp-forgejo/internal/domain"
	"example.com/teran/mcp-forgejo/internal/infrastructure/forgejo"
)

// mockForgejoBatchM answers the Batch M endpoints.
func mockForgejoBatchM() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		m := r.Method

		switch {
		case m == http.MethodGet && p == "/api/v1/repos/acme/demo/milestones":
			_, _ = w.Write([]byte(`[
				{"id":1,"title":"v1","description":"first","state":"open","open_issues":2,"closed_issues":1,"due_on":"2024-06-01"},
				{"id":2,"title":"v2","description":"second","state":"closed","open_issues":0,"closed_issues":5,"due_on":""}
			]`))
			return
		case m == http.MethodPost && p == "/api/v1/repos/acme/demo/milestones":
			_, _ = w.Write([]byte(`{"id":3,"title":"v3","description":"third","state":"open","open_issues":0,"closed_issues":0,"due_on":"2024-07-01"}`))
			return
		case m == http.MethodPatch && p == "/api/v1/repos/acme/demo/milestones/1":
			_, _ = w.Write([]byte(`{"id":1,"title":"v1r","description":"updated","state":"closed","open_issues":0,"closed_issues":3,"due_on":"2024-08-01"}`))
			return
		case m == http.MethodDelete && p == "/api/v1/repos/acme/demo/milestones/1":
			w.WriteHeader(http.StatusNoContent)
			return
		case m == http.MethodGet && p == "/api/v1/repos/acme/demo/labels":
			_, _ = w.Write([]byte(`[
				{"id":1,"name":"bug","color":"d73a4a","description":"a bug"},
				{"id":2,"name":"enhancement","color":"a2eeef","description":"new feature"}
			]`))
			return
		case m == http.MethodPost && p == "/api/v1/repos/acme/demo/labels":
			_, _ = w.Write([]byte(`{"id":3,"name":"docs","color":"0e8a16","description":"documentation"}`))
			return
		case m == http.MethodPatch && p == "/api/v1/repos/acme/demo/labels/1":
			_, _ = w.Write([]byte(`{"id":1,"name":"bugfix","color":"000000","description":"fixed"}`))
			return
		case m == http.MethodDelete && p == "/api/v1/repos/acme/demo/labels/1":
			w.WriteHeader(http.StatusNoContent)
			return
		case m == http.MethodPut && p == "/api/v1/repos/acme/demo/issues/5/labels":
			w.WriteHeader(http.StatusNoContent)
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"unexpected "` + m + `" ` + p + `"}`))
		}
	})
}

// setupBatchM builds the MCP server backed by mockForgejoBatchM and returns a
// connected client session.
func setupBatchM(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ts := httptest.NewServer(mockForgejoBatchM())
	t.Cleanup(ts.Close)

	s, err := Build(forgejo.Config{BaseURL: ts.URL, Token: testToken})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

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
	return cs
}

// batchMWriteIdempotent maps each Batch M write tool to its expected
// idempotentHint: true only for milestone_update, label_update and
// issue_set_labels (setting the exact label set is a no-op when repeated);
// the create tools are not idempotent.
var batchMWriteIdempotent = map[string]bool{
	"forgejo_milestone_create": false,
	"forgejo_milestone_update": true,
	"forgejo_label_create":     false,
	"forgejo_label_update":     true,
	"forgejo_issue_set_labels": true,
}

func TestBatchMReadToolsRegistered(t *testing.T) {
	cs, _ := setup(t) // registration is transport-independent
	names := listToolMap(t, cs)
	for _, name := range []string{"forgejo_milestone_list", "forgejo_label_list"} {
		if _, ok := names[name]; !ok {
			t.Errorf("read tool %q not registered", name)
		}
	}
}

func TestBatchMWriteToolsRegistered(t *testing.T) {
	cs, _ := setup(t)
	names := listToolMap(t, cs)
	for name := range batchMWriteIdempotent {
		if _, ok := names[name]; !ok {
			t.Errorf("write tool %q not registered", name)
		}
	}
	for _, name := range []string{"forgejo_milestone_delete", "forgejo_label_delete"} {
		if _, ok := names[name]; !ok {
			t.Errorf("delete tool %q not registered", name)
		}
	}
}

func TestBatchMReadAnnotations(t *testing.T) {
	cs, _ := setup(t)
	byName := listToolMap(t, cs)
	for _, name := range []string{"forgejo_milestone_list", "forgejo_label_list"} {
		tool, ok := byName[name]
		if !ok {
			t.Errorf("tool %q not registered", name)
			continue
		}
		a := tool.Annotations
		if a == nil {
			t.Errorf("tool %q: annotations missing", name)
			continue
		}
		if a.Title == "" {
			t.Errorf("tool %q: annotations.title empty", name)
		}
		if !a.ReadOnlyHint || !a.IdempotentHint {
			t.Errorf("tool %q: readOnlyHint=%v idempotentHint=%v, want true/true", name, a.ReadOnlyHint, a.IdempotentHint)
		}
		if a.DestructiveHint == nil || *a.DestructiveHint {
			t.Errorf("tool %q: destructiveHint != false", name)
		}
		if a.OpenWorldHint == nil || *a.OpenWorldHint {
			t.Errorf("tool %q: openWorldHint != false", name)
		}
	}
}

func TestBatchMWriteAnnotations(t *testing.T) {
	cs, _ := setup(t)
	byName := listToolMap(t, cs)
	for name, wantIdem := range batchMWriteIdempotent {
		tool, ok := byName[name]
		if !ok {
			t.Errorf("tool %q not registered", name)
			continue
		}
		a := tool.Annotations
		if a == nil {
			t.Errorf("tool %q: annotations missing", name)
			continue
		}
		if a.Title == "" {
			t.Errorf("tool %q: annotations.title empty", name)
		}
		if a.ReadOnlyHint {
			t.Errorf("tool %q: readOnlyHint != false", name)
		}
		if a.DestructiveHint == nil || *a.DestructiveHint {
			t.Errorf("tool %q: destructiveHint != false", name)
		}
		if a.OpenWorldHint == nil || *a.OpenWorldHint {
			t.Errorf("tool %q: openWorldHint != false", name)
		}
		if a.IdempotentHint != wantIdem {
			t.Errorf("tool %q: idempotentHint = %v, want %v", name, a.IdempotentHint, wantIdem)
		}
	}
}

func TestBatchMDeleteAnnotations(t *testing.T) {
	cs, _ := setup(t)
	byName := listToolMap(t, cs)
	for _, name := range []string{"forgejo_milestone_delete", "forgejo_label_delete"} {
		tool, ok := byName[name]
		if !ok {
			t.Errorf("tool %q not registered", name)
			continue
		}
		a := tool.Annotations
		if a == nil {
			t.Errorf("tool %q: annotations missing", name)
			continue
		}
		if a.Title == "" {
			t.Errorf("tool %q: annotations.title empty", name)
		}
		if a.ReadOnlyHint {
			t.Errorf("tool %q: readOnlyHint != false", name)
		}
		if a.DestructiveHint == nil || !*a.DestructiveHint {
			t.Errorf("tool %q: destructiveHint != true", name)
		}
		if a.IdempotentHint {
			t.Errorf("tool %q: idempotentHint != false", name)
		}
		if a.OpenWorldHint == nil || *a.OpenWorldHint {
			t.Errorf("tool %q: openWorldHint != false", name)
		}
		// Destructive tools surface an explicit confirmation prompt in the
		// description (SPEC 6.3).
		if !strings.Contains(tool.Description, "confirm") {
			t.Errorf("tool %q description should require confirmation, got %q", name, tool.Description)
		}
	}
}

// =============================================================================
// End-to-end invocation
// =============================================================================

func TestMilestoneListTool(t *testing.T) {
	cs := setupBatchM(t)
	var ms domain.Items[domain.Milestone]
	callTool(t, cs, "forgejo_milestone_list", map[string]any{"owner": "acme", "repo": "demo"}, &ms)
	if len(ms.Items) != 2 || ms.Items[0].Title != "v1" || ms.Items[0].OpenIssues != 2 ||
		ms.Items[1].Title != "v2" || ms.Items[1].ClosedIssues != 5 {
		t.Errorf("milestones = %+v", ms)
	}
}

func TestMilestoneCreateTool(t *testing.T) {
	cs := setupBatchM(t)
	var m domain.Milestone
	callTool(t, cs, "forgejo_milestone_create", map[string]any{
		"owner": "acme", "repo": "demo", "title": "v3", "description": "third", "due_on": "2024-07-01",
	}, &m)
	if m.ID != 3 || m.Title != "v3" || m.DueOn != "2024-07-01" {
		t.Errorf("milestone = %+v", m)
	}
}

func TestMilestoneUpdateTool(t *testing.T) {
	cs := setupBatchM(t)
	var m domain.Milestone
	callTool(t, cs, "forgejo_milestone_update", map[string]any{
		"owner": "acme", "repo": "demo", "id": 1, "title": "v1r", "state": "closed",
	}, &m)
	if m.Title != "v1r" || m.State != "closed" {
		t.Errorf("milestone = %+v", m)
	}
}

func TestMilestoneDeleteTool(t *testing.T) {
	cs := setupBatchM(t)
	callTool(t, cs, "forgejo_milestone_delete", map[string]any{
		"owner": "acme", "repo": "demo", "id": 1,
	}, nil)
}

func TestLabelListTool(t *testing.T) {
	cs := setupBatchM(t)
	var ls domain.Items[domain.Label]
	callTool(t, cs, "forgejo_label_list", map[string]any{"owner": "acme", "repo": "demo"}, &ls)
	if len(ls.Items) != 2 || ls.Items[0].Name != "bug" || ls.Items[0].Color != "d73a4a" ||
		ls.Items[1].Name != "enhancement" {
		t.Errorf("labels = %+v", ls)
	}
}

func TestLabelCreateTool(t *testing.T) {
	cs := setupBatchM(t)
	var l domain.Label
	callTool(t, cs, "forgejo_label_create", map[string]any{
		"owner": "acme", "repo": "demo", "name": "docs", "color": "0e8a16", "description": "documentation",
	}, &l)
	if l.ID != 3 || l.Name != "docs" || l.Color != "0e8a16" || l.Description != "documentation" {
		t.Errorf("label = %+v", l)
	}
}

func TestLabelUpdateTool(t *testing.T) {
	cs := setupBatchM(t)
	var l domain.Label
	callTool(t, cs, "forgejo_label_update", map[string]any{
		"owner": "acme", "repo": "demo", "id": 1, "name": "bugfix", "color": "000000",
	}, &l)
	if l.Name != "bugfix" || l.Color != "000000" {
		t.Errorf("label = %+v", l)
	}
}

func TestLabelDeleteTool(t *testing.T) {
	cs := setupBatchM(t)
	callTool(t, cs, "forgejo_label_delete", map[string]any{
		"owner": "acme", "repo": "demo", "id": 1,
	}, nil)
}

func TestIssueSetLabelsTool(t *testing.T) {
	cs := setupBatchM(t)
	callTool(t, cs, "forgejo_issue_set_labels", map[string]any{
		"owner": "acme", "repo": "demo", "index": 5, "labels": []int64{1, 2, 3},
	}, nil)
}

// =============================================================================
// Error surfacing + token redaction (SPEC S2)
// =============================================================================

func TestBatchMToolErrorSurfacesIsError(t *testing.T) {
	cs, _ := setup(t) // read mock returns 404 for unknown Batch M paths
	for _, name := range []string{
		"forgejo_milestone_list", "forgejo_milestone_create", "forgejo_milestone_update", "forgejo_milestone_delete",
		"forgejo_label_list", "forgejo_label_create", "forgejo_label_update", "forgejo_label_delete",
		"forgejo_issue_set_labels",
	} {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
			Name:      name,
			Arguments: map[string]any{"owner": "ghost", "repo": "missing"},
		})
		if err != nil {
			t.Fatalf("CallTool(%s) error = %v", name, err)
		}
		if !res.IsError {
			t.Errorf("%s: expected IsError for 404", name)
		}
		msg := textOf(t, res)
		if strings.Contains(msg, testToken) {
			t.Errorf("%s: token leaked in error: %q", name, msg)
		}
	}
}

// =============================================================================
// List object contract: milestone_list / label_list return a JSON object
// ({items:[...]})
// =============================================================================

func TestBatchMListReturnsObjectContract(t *testing.T) {
	cs := setupBatchM(t)
	for _, name := range []string{"forgejo_milestone_list", "forgejo_label_list"} {
		res := callToolRaw(t, cs, name, map[string]any{"owner": "acme", "repo": "demo"})
		items := listStructuredItems(t, res)
		if len(items) != 2 {
			t.Errorf("%s items length = %d, want 2", name, len(items))
		}
	}
}

// =============================================================================
// JSON schema field pins
// =============================================================================

func TestBatchMToolJSONSchemaPinsFields(t *testing.T) {
	cs, _ := setup(t)
	byName := listToolMap(t, cs)

	check := func(toolName string, wantFields []string) {
		t.Helper()
		tool, ok := byName[toolName]
		if !ok {
			t.Errorf("tool %q not registered", toolName)
			return
		}
		schemaJSON, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal schema for %s: %v", toolName, err)
		}
		for _, f := range wantFields {
			if !strings.Contains(string(schemaJSON), `"`+f+`"`) {
				t.Errorf("tool %q schema missing field %q (schema=%s)", toolName, f, schemaJSON)
			}
		}
	}

	check("forgejo_milestone_list", []string{"owner", "repo"})
	check("forgejo_milestone_create", []string{"owner", "repo", "title", "description", "due_on"})
	check("forgejo_milestone_update", []string{"owner", "repo", "id", "title", "description", "state", "due_on"})
	check("forgejo_milestone_delete", []string{"owner", "repo", "id"})
	check("forgejo_label_list", []string{"owner", "repo"})
	check("forgejo_label_create", []string{"owner", "repo", "name", "color", "description"})
	check("forgejo_label_update", []string{"owner", "repo", "id", "name", "color", "description"})
	check("forgejo_label_delete", []string{"owner", "repo", "id"})
	check("forgejo_issue_set_labels", []string{"owner", "repo", "index", "labels"})
}
