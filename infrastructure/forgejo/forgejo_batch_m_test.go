package forgejo

// Batch M client tests (SPEC §6): forgejo_milestone_list/create/update/delete,
// forgejo_label_list/create/update/delete, forgejo_issue_set_labels. Test
// expectations for @developer: endpoints, HTTP methods, request bodies and the
// domain types/methods they must add. Read the header of forgejo_test.go for
// the shared helpers (newTestServer, testToken) these tests build on.
//
// @developer must add to domain:
//   - type Milestone struct { ID int64; Title, Description, State, DueOn string;
//     OpenIssues, ClosedIssues int } with json tags
//     id/title/description/state/open_issues/closed_issues/due_on
//   - type CreateMilestoneInput struct { Title, Description, DueOn string }
//     with json tags title/description/due_on (description/due_on omitempty)
//   - type UpdateMilestoneInput struct { Title, Description, State, DueOn string }
//     with json tags title/description/state/due_on (all omitempty)
//   - type CreateLabelInput struct { Name, Color, Description string }
//     with json tags name/color/description (color/description omitempty)
//   - type UpdateLabelInput struct { Name, Color, Description string }
//     with json tags name/color/description (all omitempty)
//   - add Description string `json:"description"` to the existing Label struct
//     (ID int64; Name, Color, Description string; json id/name/color/description)
//
// and the client methods:
//   - ListMilestones(ctx, owner, repo) ([]Milestone, error)
//     GET  /api/v1/repos/{o}/{r}/milestones
//   - CreateMilestone(ctx, owner, repo, CreateMilestoneInput) (Milestone, error)
//     POST /api/v1/repos/{o}/{r}/milestones
//   - UpdateMilestone(ctx, owner, repo, id, UpdateMilestoneInput) (Milestone, error)
//     PATCH /api/v1/repos/{o}/{r}/milestones/{id}
//   - DeleteMilestone(ctx, owner, repo, id) error
//     DELETE /api/v1/repos/{o}/{r}/milestones/{id} (204)
//   - ListLabels(ctx, owner, repo) ([]Label, error)
//     GET  /api/v1/repos/{o}/{r}/labels
//   - CreateLabel(ctx, owner, repo, CreateLabelInput) (Label, error)
//     POST /api/v1/repos/{o}/{r}/labels
//   - UpdateLabel(ctx, owner, repo, id, UpdateLabelInput) (Label, error)
//     PATCH /api/v1/repos/{o}/{r}/labels/{id}
//   - DeleteLabel(ctx, owner, repo, id) error
//     DELETE /api/v1/repos/{o}/{r}/labels/{id} (204)
//   - SetIssueLabels(ctx, owner, repo, index, []int64) error
//     PUT /api/v1/repos/{o}/{r}/issues/{index}/labels, body {"labels":[...]}

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/teran/mcp-forgejo/domain"
)

// =============================================================================
// forgejo_milestone_list — issueGetMilestonesList
// =============================================================================

func TestListMilestones(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`[
			{"id":1,"title":"v1","description":"first","state":"open","open_issues":2,"closed_issues":1,"due_on":"2024-06-01"},
			{"id":2,"title":"v2","description":"second","state":"closed","open_issues":0,"closed_issues":5,"due_on":""}
		]`))
	})
	c, _ := newTestServer(t, handler)

	ms, err := c.ListMilestones(context.Background(), "acme", "demo")
	if err != nil {
		t.Fatalf("ListMilestones() error = %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/milestones" {
		t.Errorf("path = %q, want /api/v1/repos/acme/demo/milestones", gotPath)
	}
	if gotAuth != "token "+testToken {
		t.Errorf("auth = %q", gotAuth)
	}
	if len(ms) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(ms), ms)
	}
	if ms[0].ID != 1 || ms[0].Title != "v1" || ms[0].Description != "first" || ms[0].State != "open" ||
		ms[0].OpenIssues != 2 || ms[0].ClosedIssues != 1 || ms[0].DueOn != "2024-06-01" {
		t.Errorf("milestones[0] = %+v", ms[0])
	}
	if ms[1].ID != 2 || ms[1].Title != "v2" || ms[1].State != "closed" || ms[1].ClosedIssues != 5 {
		t.Errorf("milestones[1] = %+v", ms[1])
	}
}

func TestListMilestonesEmpty(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	c, _ := newTestServer(t, handler)
	ms, err := c.ListMilestones(context.Background(), "acme", "demo")
	if err != nil {
		t.Fatalf("ListMilestones() error = %v", err)
	}
	if len(ms) != 0 {
		t.Errorf("expected empty milestones, got %+v", ms)
	}
}

func TestListMilestonesNotFound(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	c, _ := newTestServer(t, handler)
	_, err := c.ListMilestones(context.Background(), "acme", "demo")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok || fe.Kind != domain.KindNotFound {
		t.Errorf("expected not-found, got %v", err)
	}
}

// =============================================================================
// forgejo_milestone_create — issueCreateMilestone
// =============================================================================

func TestCreateMilestone(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"id":3,"title":"v3","description":"third","state":"open","open_issues":0,"closed_issues":0,"due_on":"2024-07-01"}`))
	})
	c, _ := newTestServer(t, handler)

	m, err := c.CreateMilestone(context.Background(), "acme", "demo", domain.CreateMilestoneInput{
		Title: "v3", Description: "third", DueOn: "2024-07-01",
	})
	if err != nil {
		t.Fatalf("CreateMilestone() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/milestones" {
		t.Errorf("path = %q, want /api/v1/repos/acme/demo/milestones", gotPath)
	}
	if gotBody["title"] != "v3" || gotBody["description"] != "third" || gotBody["due_on"] != "2024-07-01" {
		t.Errorf("body = %+v, want title/description/due_on", gotBody)
	}
	if m.ID != 3 || m.Title != "v3" || m.DueOn != "2024-07-01" {
		t.Errorf("milestone = %+v", m)
	}
}

func TestCreateMilestoneOmitsEmptyFields(t *testing.T) {
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"id":3,"title":"v3"}`))
	})
	c, _ := newTestServer(t, handler)

	_, err := c.CreateMilestone(context.Background(), "acme", "demo", domain.CreateMilestoneInput{Title: "v3"})
	if err != nil {
		t.Fatalf("CreateMilestone() error = %v", err)
	}
	for _, f := range []string{"description", "due_on"} {
		if _, ok := gotBody[f]; ok {
			t.Errorf("body contains %q but it should be omitted when empty: %+v", f, gotBody)
		}
	}
	if gotBody["title"] != "v3" {
		t.Errorf("body title = %+v", gotBody)
	}
}

// =============================================================================
// forgejo_milestone_update — issueEditMilestone (idempotent)
// =============================================================================

func TestUpdateMilestone(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"id":1,"title":"v1r","description":"updated","state":"closed","open_issues":0,"closed_issues":3,"due_on":"2024-08-01"}`))
	})
	c, _ := newTestServer(t, handler)

	m, err := c.UpdateMilestone(context.Background(), "acme", "demo", 1, domain.UpdateMilestoneInput{
		Title: "v1r", Description: "updated", State: "closed", DueOn: "2024-08-01",
	})
	if err != nil {
		t.Fatalf("UpdateMilestone() error = %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/milestones/1" {
		t.Errorf("path = %q, want /api/v1/repos/acme/demo/milestones/1", gotPath)
	}
	if gotBody["title"] != "v1r" || gotBody["description"] != "updated" || gotBody["state"] != "closed" || gotBody["due_on"] != "2024-08-01" {
		t.Errorf("body = %+v, want title/description/state/due_on", gotBody)
	}
	if m.ID != 1 || m.Title != "v1r" || m.State != "closed" {
		t.Errorf("milestone = %+v", m)
	}
}

func TestUpdateMilestoneOmitsEmptyFields(t *testing.T) {
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"id":1,"title":"v1"}`))
	})
	c, _ := newTestServer(t, handler)

	_, err := c.UpdateMilestone(context.Background(), "acme", "demo", 1, domain.UpdateMilestoneInput{})
	if err != nil {
		t.Fatalf("UpdateMilestone() error = %v", err)
	}
	for _, f := range []string{"title", "description", "state", "due_on"} {
		if _, ok := gotBody[f]; ok {
			t.Errorf("body contains %q but it should be omitted when empty: %+v", f, gotBody)
		}
	}
}

// =============================================================================
// forgejo_milestone_delete — issueDeleteMilestone
// =============================================================================

func TestDeleteMilestone(t *testing.T) {
	var gotMethod, gotPath string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	c, _ := newTestServer(t, handler)

	if err := c.DeleteMilestone(context.Background(), "acme", "demo", 1); err != nil {
		t.Fatalf("DeleteMilestone() error = %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/milestones/1" {
		t.Errorf("path = %q, want /api/v1/repos/acme/demo/milestones/1", gotPath)
	}
}

// =============================================================================
// forgejo_label_list — issueListLabels
// =============================================================================

func TestListLabels(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`[
			{"id":1,"name":"bug","color":"d73a4a","description":"a bug"},
			{"id":2,"name":"enhancement","color":"a2eeef","description":"new feature"}
		]`))
	})
	c, _ := newTestServer(t, handler)

	ls, err := c.ListLabels(context.Background(), "acme", "demo")
	if err != nil {
		t.Fatalf("ListLabels() error = %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/labels" {
		t.Errorf("path = %q, want /api/v1/repos/acme/demo/labels", gotPath)
	}
	if gotAuth != "token "+testToken {
		t.Errorf("auth = %q", gotAuth)
	}
	if len(ls) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(ls), ls)
	}
	if ls[0].ID != 1 || ls[0].Name != "bug" || ls[0].Color != "d73a4a" || ls[0].Description != "a bug" {
		t.Errorf("labels[0] = %+v", ls[0])
	}
	if ls[1].Name != "enhancement" || ls[1].Color != "a2eeef" {
		t.Errorf("labels[1] = %+v", ls[1])
	}
}

func TestListLabelsEmpty(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	c, _ := newTestServer(t, handler)
	ls, err := c.ListLabels(context.Background(), "acme", "demo")
	if err != nil {
		t.Fatalf("ListLabels() error = %v", err)
	}
	if len(ls) != 0 {
		t.Errorf("expected empty labels, got %+v", ls)
	}
}

func TestListLabelsNotFound(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	c, _ := newTestServer(t, handler)
	_, err := c.ListLabels(context.Background(), "acme", "demo")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	fe, ok := err.(*domain.ForgejoError)
	if !ok || fe.Kind != domain.KindNotFound {
		t.Errorf("expected not-found, got %v", err)
	}
}

// =============================================================================
// forgejo_label_create — issueCreateLabel
// =============================================================================

func TestCreateLabel(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"id":3,"name":"docs","color":"0e8a16","description":"documentation"}`))
	})
	c, _ := newTestServer(t, handler)

	l, err := c.CreateLabel(context.Background(), "acme", "demo", domain.CreateLabelInput{
		Name: "docs", Color: "0e8a16", Description: "documentation",
	})
	if err != nil {
		t.Fatalf("CreateLabel() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/labels" {
		t.Errorf("path = %q, want /api/v1/repos/acme/demo/labels", gotPath)
	}
	if gotBody["name"] != "docs" || gotBody["color"] != "0e8a16" || gotBody["description"] != "documentation" {
		t.Errorf("body = %+v, want name/color/description", gotBody)
	}
	if l.ID != 3 || l.Name != "docs" || l.Color != "0e8a16" || l.Description != "documentation" {
		t.Errorf("label = %+v", l)
	}
}

func TestCreateLabelOmitsEmptyFields(t *testing.T) {
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"id":3,"name":"docs"}`))
	})
	c, _ := newTestServer(t, handler)

	_, err := c.CreateLabel(context.Background(), "acme", "demo", domain.CreateLabelInput{Name: "docs"})
	if err != nil {
		t.Fatalf("CreateLabel() error = %v", err)
	}
	for _, f := range []string{"color", "description"} {
		if _, ok := gotBody[f]; ok {
			t.Errorf("body contains %q but it should be omitted when empty: %+v", f, gotBody)
		}
	}
	if gotBody["name"] != "docs" {
		t.Errorf("body name = %+v", gotBody)
	}
}

// =============================================================================
// forgejo_label_update — issueEditLabel (idempotent)
// =============================================================================

func TestUpdateLabel(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"id":1,"name":"bugfix","color":"000000","description":"fixed"}`))
	})
	c, _ := newTestServer(t, handler)

	l, err := c.UpdateLabel(context.Background(), "acme", "demo", 1, domain.UpdateLabelInput{
		Name: "bugfix", Color: "000000", Description: "fixed",
	})
	if err != nil {
		t.Fatalf("UpdateLabel() error = %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/labels/1" {
		t.Errorf("path = %q, want /api/v1/repos/acme/demo/labels/1", gotPath)
	}
	if gotBody["name"] != "bugfix" || gotBody["color"] != "000000" || gotBody["description"] != "fixed" {
		t.Errorf("body = %+v, want name/color/description", gotBody)
	}
	if l.ID != 1 || l.Name != "bugfix" || l.Color != "000000" {
		t.Errorf("label = %+v", l)
	}
}

func TestUpdateLabelOmitsEmptyFields(t *testing.T) {
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = decodeBody(t, r)
		_, _ = w.Write([]byte(`{"id":1,"name":"bug"}`))
	})
	c, _ := newTestServer(t, handler)

	_, err := c.UpdateLabel(context.Background(), "acme", "demo", 1, domain.UpdateLabelInput{})
	if err != nil {
		t.Fatalf("UpdateLabel() error = %v", err)
	}
	for _, f := range []string{"name", "color", "description"} {
		if _, ok := gotBody[f]; ok {
			t.Errorf("body contains %q but it should be omitted when empty: %+v", f, gotBody)
		}
	}
}

// =============================================================================
// forgejo_label_delete — issueDeleteLabel
// =============================================================================

func TestDeleteLabel(t *testing.T) {
	var gotMethod, gotPath string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})
	c, _ := newTestServer(t, handler)

	if err := c.DeleteLabel(context.Background(), "acme", "demo", 1); err != nil {
		t.Fatalf("DeleteLabel() error = %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/labels/1" {
		t.Errorf("path = %q, want /api/v1/repos/acme/demo/labels/1", gotPath)
	}
}

// =============================================================================
// forgejo_issue_set_labels — issueReplaceLabels (idempotent, exact set)
// =============================================================================

func TestSetIssueLabels(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody = decodeBody(t, r)
		w.WriteHeader(http.StatusNoContent)
	})
	c, _ := newTestServer(t, handler)

	if err := c.SetIssueLabels(context.Background(), "acme", "demo", 5, []int64{1, 2, 3}); err != nil {
		t.Fatalf("SetIssueLabels() error = %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if gotPath != "/api/v1/repos/acme/demo/issues/5/labels" {
		t.Errorf("path = %q, want /api/v1/repos/acme/demo/issues/5/labels", gotPath)
	}
	labels, ok := gotBody["labels"].([]any)
	if !ok || len(labels) != 3 || labels[0].(float64) != 1 || labels[1].(float64) != 2 || labels[2].(float64) != 3 {
		t.Errorf("body = %+v, want labels:[1,2,3]", gotBody)
	}
}

func TestSetIssueLabelsEmptySet(t *testing.T) {
	var gotBody map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = decodeBody(t, r)
		w.WriteHeader(http.StatusNoContent)
	})
	c, _ := newTestServer(t, handler)

	if err := c.SetIssueLabels(context.Background(), "acme", "demo", 5, nil); err != nil {
		t.Fatalf("SetIssueLabels() error = %v", err)
	}
	labels, ok := gotBody["labels"].([]any)
	if !ok || len(labels) != 0 {
		t.Errorf("body = %+v, want empty labels array", gotBody)
	}
}

// =============================================================================
// Batch M client: every method must surface 5xx as transient and never leak
// the token (SPEC S2).
// =============================================================================

func TestBatchMMethodsServerError(t *testing.T) {
	body := `{"message":"server boom ` + testToken + `"}`
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(body))
	})
	c, _ := newTestServer(t, handler)

	calls := []struct {
		name string
		call func() error
	}{
		{"ListMilestones", func() error { _, err := c.ListMilestones(context.Background(), "acme", "demo"); return err }},
		{"CreateMilestone", func() error {
			_, err := c.CreateMilestone(context.Background(), "acme", "demo", domain.CreateMilestoneInput{Title: "v1"})
			return err
		}},
		{"UpdateMilestone", func() error {
			_, err := c.UpdateMilestone(context.Background(), "acme", "demo", 1, domain.UpdateMilestoneInput{Title: "v1"})
			return err
		}},
		{"DeleteMilestone", func() error { return c.DeleteMilestone(context.Background(), "acme", "demo", 1) }},
		{"ListLabels", func() error { _, err := c.ListLabels(context.Background(), "acme", "demo"); return err }},
		{"CreateLabel", func() error {
			_, err := c.CreateLabel(context.Background(), "acme", "demo", domain.CreateLabelInput{Name: "bug"})
			return err
		}},
		{"UpdateLabel", func() error {
			_, err := c.UpdateLabel(context.Background(), "acme", "demo", 1, domain.UpdateLabelInput{Name: "bug"})
			return err
		}},
		{"DeleteLabel", func() error { return c.DeleteLabel(context.Background(), "acme", "demo", 1) }},
		{"SetIssueLabels", func() error { return c.SetIssueLabels(context.Background(), "acme", "demo", 5, []int64{1}) }},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatal("expected error for 5xx")
			}
			fe, ok := err.(*domain.ForgejoError)
			if !ok {
				t.Fatalf("expected *domain.ForgejoError, got %T", err)
			}
			if fe.Kind != domain.KindTransient {
				t.Errorf("Kind = %q, want transient", fe.Kind)
			}
			if strings.Contains(err.Error(), testToken) || !strings.Contains(err.Error(), "[REDACTED]") {
				t.Errorf("token not redacted: %q", err.Error())
			}
		})
	}
}

// TestBatchMResponseBodyReadError verifies the read methods also surface a
// failing response-body read as transient.
func TestBatchMResponseBodyReadError(t *testing.T) {
	c := NewWithClient(Config{BaseURL: "https://git.example.dev", Token: testToken}, &http.Client{Transport: errTransport{}})
	calls := []struct {
		name string
		call func() error
	}{
		{"ListMilestones", func() error { _, err := c.ListMilestones(context.Background(), "acme", "demo"); return err }},
		{"ListLabels", func() error { _, err := c.ListLabels(context.Background(), "acme", "demo"); return err }},
	}
	for _, tc := range calls {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatal("expected error for failing response body read")
			}
			fe, ok := err.(*domain.ForgejoError)
			if !ok || fe.Kind != domain.KindTransient {
				t.Errorf("expected transient, got %v", err)
			}
		})
	}
}
