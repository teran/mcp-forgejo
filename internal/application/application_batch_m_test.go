package application

// Batch M use cases (SPEC §6): forgejo_milestone_list/create/update/delete,
// forgejo_label_list/create/update/delete, forgejo_issue_set_labels. Test
// expectations for @developer: the application functions (orchestration +
// validation) and the domain interfaces/types they must call. The stub services
// below implement the domain interfaces @developer must add to internal/domain.

import (
	"context"
	"errors"
	"testing"

	"github.com/teran/mcp-forgejo/internal/domain"
)

// --- stubs for the new domain interfaces ------------------------------------

type stubMilestoneService struct {
	milestones        []domain.Milestone
	err               error
	calls             int
	gotOwner, gotRepo string
}

func (s *stubMilestoneService) ListMilestones(_ context.Context, owner, repo string) ([]domain.Milestone, error) {
	s.calls++
	s.gotOwner, s.gotRepo = owner, repo
	return s.milestones, s.err
}

type stubMilestoneWriteService struct {
	milestone         domain.Milestone
	err               error
	calls             int
	gotOwner, gotRepo string
	gotID             int64
	gotCreateIn       domain.CreateMilestoneInput
	gotUpdateIn       domain.UpdateMilestoneInput
}

func (s *stubMilestoneWriteService) CreateMilestone(_ context.Context, owner, repo string, in domain.CreateMilestoneInput) (domain.Milestone, error) {
	s.calls++
	s.gotOwner, s.gotRepo = owner, repo
	s.gotCreateIn = in
	return s.milestone, s.err
}

func (s *stubMilestoneWriteService) UpdateMilestone(_ context.Context, owner, repo string, id int64, in domain.UpdateMilestoneInput) (domain.Milestone, error) {
	s.calls++
	s.gotOwner, s.gotRepo = owner, repo
	s.gotID = id
	s.gotUpdateIn = in
	return s.milestone, s.err
}

type stubMilestoneDeleteService struct {
	err               error
	calls             int
	gotOwner, gotRepo string
	gotID             int64
}

func (s *stubMilestoneDeleteService) DeleteMilestone(_ context.Context, owner, repo string, id int64) error {
	s.calls++
	s.gotOwner, s.gotRepo = owner, repo
	s.gotID = id
	return s.err
}

type stubLabelService struct {
	labels            []domain.Label
	err               error
	calls             int
	gotOwner, gotRepo string
}

func (s *stubLabelService) ListLabels(_ context.Context, owner, repo string) ([]domain.Label, error) {
	s.calls++
	s.gotOwner, s.gotRepo = owner, repo
	return s.labels, s.err
}

type stubLabelWriteService struct {
	label             domain.Label
	err               error
	calls             int
	gotOwner, gotRepo string
	gotID             int64
	gotCreateIn       domain.CreateLabelInput
	gotUpdateIn       domain.UpdateLabelInput
}

func (s *stubLabelWriteService) CreateLabel(_ context.Context, owner, repo string, in domain.CreateLabelInput) (domain.Label, error) {
	s.calls++
	s.gotOwner, s.gotRepo = owner, repo
	s.gotCreateIn = in
	return s.label, s.err
}

func (s *stubLabelWriteService) UpdateLabel(_ context.Context, owner, repo string, id int64, in domain.UpdateLabelInput) (domain.Label, error) {
	s.calls++
	s.gotOwner, s.gotRepo = owner, repo
	s.gotID = id
	s.gotUpdateIn = in
	return s.label, s.err
}

type stubLabelDeleteService struct {
	err               error
	calls             int
	gotOwner, gotRepo string
	gotID             int64
}

func (s *stubLabelDeleteService) DeleteLabel(_ context.Context, owner, repo string, id int64) error {
	s.calls++
	s.gotOwner, s.gotRepo = owner, repo
	s.gotID = id
	return s.err
}

type stubIssueLabelsService struct {
	err               error
	calls             int
	gotOwner, gotRepo string
	gotIndex          int64
	gotLabelIDs       []int64
}

func (s *stubIssueLabelsService) SetIssueLabels(_ context.Context, owner, repo string, index int64, labelIDs []int64) error {
	s.calls++
	s.gotOwner, s.gotRepo = owner, repo
	s.gotIndex = index
	s.gotLabelIDs = labelIDs
	return s.err
}

// =============================================================================
// forgejo_milestone_list — passthrough
// =============================================================================

func TestListMilestones(t *testing.T) {
	svc := &stubMilestoneService{milestones: []domain.Milestone{{ID: 1, Title: "v1"}}}
	got, err := ListMilestones(context.Background(), svc, "acme", "demo")
	if err != nil {
		t.Fatalf("ListMilestones() error = %v", err)
	}
	if len(got) != 1 || got[0].Title != "v1" || got[0].ID != 1 {
		t.Errorf("milestones = %+v", got)
	}
	if svc.calls != 1 || svc.gotOwner != "acme" || svc.gotRepo != "demo" {
		t.Errorf("args not forwarded: %+v", svc)
	}
}

func TestListMilestonesPropagatesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindNotFound, "missing")
	svc := &stubMilestoneService{err: want}
	if _, err := ListMilestones(context.Background(), svc, "acme", "demo"); !errors.Is(err, want) {
		t.Errorf("ListMilestones() error = %v, want %v", err, want)
	}
}

// =============================================================================
// forgejo_milestone_create — validation (title required)
// =============================================================================

func TestCreateMilestone(t *testing.T) {
	svc := &stubMilestoneWriteService{milestone: domain.Milestone{ID: 3, Title: "v3"}}
	got, err := CreateMilestone(context.Background(), svc, "acme", "demo", domain.CreateMilestoneInput{
		Title: "v3", Description: "third", DueOn: "2024-07-01",
	})
	if err != nil {
		t.Fatalf("CreateMilestone() error = %v", err)
	}
	if got.Title != "v3" {
		t.Errorf("milestone = %+v", got)
	}
	if svc.calls != 1 || svc.gotOwner != "acme" || svc.gotRepo != "demo" {
		t.Errorf("args not forwarded: %+v", svc)
	}
	if svc.gotCreateIn.Title != "v3" || svc.gotCreateIn.Description != "third" || svc.gotCreateIn.DueOn != "2024-07-01" {
		t.Errorf("input not forwarded: %+v", svc.gotCreateIn)
	}
}

func TestCreateMilestonePropagatesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindConflict, "exists")
	svc := &stubMilestoneWriteService{err: want}
	if _, err := CreateMilestone(context.Background(), svc, "acme", "demo", domain.CreateMilestoneInput{Title: "v3"}); !errors.Is(err, want) {
		t.Errorf("CreateMilestone() error = %v, want %v", err, want)
	}
}

func TestCreateMilestoneValidation(t *testing.T) {
	svc := &stubMilestoneWriteService{}
	_, err := CreateMilestone(context.Background(), svc, "acme", "demo", domain.CreateMilestoneInput{})
	if err == nil || !isValidation(err) {
		t.Errorf("expected validation error for empty milestone title, got %v", err)
	}
	if svc.calls != 0 {
		t.Errorf("service should not be called on validation failure")
	}
}

// =============================================================================
// forgejo_milestone_update — passthrough (idempotent, no validation)
// =============================================================================

func TestUpdateMilestone(t *testing.T) {
	svc := &stubMilestoneWriteService{milestone: domain.Milestone{ID: 1, Title: "v1r"}}
	got, err := UpdateMilestone(context.Background(), svc, "acme", "demo", 1, domain.UpdateMilestoneInput{
		Title: "v1r", State: "closed",
	})
	if err != nil {
		t.Fatalf("UpdateMilestone() error = %v", err)
	}
	if got.Title != "v1r" {
		t.Errorf("milestone = %+v", got)
	}
	if svc.calls != 1 || svc.gotOwner != "acme" || svc.gotRepo != "demo" || svc.gotID != 1 {
		t.Errorf("args not forwarded: %+v", svc)
	}
	if svc.gotUpdateIn.Title != "v1r" || svc.gotUpdateIn.State != "closed" {
		t.Errorf("input not forwarded: %+v", svc.gotUpdateIn)
	}
}

func TestUpdateMilestonePropagatesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindForbidden, "no permission")
	svc := &stubMilestoneWriteService{err: want}
	if _, err := UpdateMilestone(context.Background(), svc, "acme", "demo", 1, domain.UpdateMilestoneInput{}); !errors.Is(err, want) {
		t.Errorf("UpdateMilestone() error = %v, want %v", err, want)
	}
}

func TestUpdateMilestoneIdempotent(t *testing.T) {
	svc := &stubMilestoneWriteService{milestone: domain.Milestone{ID: 1}}
	in := domain.UpdateMilestoneInput{Title: "v1"}
	for i := 0; i < 2; i++ {
		if _, err := UpdateMilestone(context.Background(), svc, "acme", "demo", 1, in); err != nil {
			t.Fatalf("UpdateMilestone() iteration %d error = %v", i, err)
		}
	}
	if svc.calls != 2 {
		t.Errorf("expected 2 idempotent calls, got %d", svc.calls)
	}
}

// =============================================================================
// forgejo_milestone_delete — validation (id > 0)
// =============================================================================

func TestDeleteMilestone(t *testing.T) {
	svc := &stubMilestoneDeleteService{}
	if err := DeleteMilestone(context.Background(), svc, "acme", "demo", 1); err != nil {
		t.Fatalf("DeleteMilestone() error = %v", err)
	}
	if svc.calls != 1 || svc.gotOwner != "acme" || svc.gotRepo != "demo" || svc.gotID != 1 {
		t.Errorf("args not forwarded: %+v", svc)
	}
}

func TestDeleteMilestonePropagatesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindNotFound, "missing")
	svc := &stubMilestoneDeleteService{err: want}
	if err := DeleteMilestone(context.Background(), svc, "acme", "demo", 1); !errors.Is(err, want) {
		t.Errorf("DeleteMilestone() error = %v, want %v", err, want)
	}
}

func TestDeleteMilestoneValidation(t *testing.T) {
	svc := &stubMilestoneDeleteService{}
	if err := DeleteMilestone(context.Background(), svc, "acme", "demo", 0); err == nil || !isValidation(err) {
		t.Errorf("expected validation error for non-positive id, got %v", err)
	}
	if svc.calls != 0 {
		t.Errorf("service should not be called on validation failure")
	}
}

// =============================================================================
// forgejo_label_list — passthrough
// =============================================================================

func TestListLabels(t *testing.T) {
	svc := &stubLabelService{labels: []domain.Label{{ID: 1, Name: "bug", Color: "d73a4a"}}}
	got, err := ListLabels(context.Background(), svc, "acme", "demo")
	if err != nil {
		t.Fatalf("ListLabels() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "bug" || got[0].Color != "d73a4a" {
		t.Errorf("labels = %+v", got)
	}
	if svc.calls != 1 || svc.gotOwner != "acme" || svc.gotRepo != "demo" {
		t.Errorf("args not forwarded: %+v", svc)
	}
}

func TestListLabelsPropagatesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindNotFound, "missing")
	svc := &stubLabelService{err: want}
	if _, err := ListLabels(context.Background(), svc, "acme", "demo"); !errors.Is(err, want) {
		t.Errorf("ListLabels() error = %v, want %v", err, want)
	}
}

// =============================================================================
// forgejo_label_create — validation (name required)
// =============================================================================

func TestCreateLabel(t *testing.T) {
	svc := &stubLabelWriteService{label: domain.Label{ID: 3, Name: "docs"}}
	got, err := CreateLabel(context.Background(), svc, "acme", "demo", domain.CreateLabelInput{
		Name: "docs", Color: "0e8a16", Description: "documentation",
	})
	if err != nil {
		t.Fatalf("CreateLabel() error = %v", err)
	}
	if got.Name != "docs" {
		t.Errorf("label = %+v", got)
	}
	if svc.calls != 1 || svc.gotOwner != "acme" || svc.gotRepo != "demo" {
		t.Errorf("args not forwarded: %+v", svc)
	}
	if svc.gotCreateIn.Name != "docs" || svc.gotCreateIn.Color != "0e8a16" || svc.gotCreateIn.Description != "documentation" {
		t.Errorf("input not forwarded: %+v", svc.gotCreateIn)
	}
}

func TestCreateLabelPropagatesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindConflict, "exists")
	svc := &stubLabelWriteService{err: want}
	if _, err := CreateLabel(context.Background(), svc, "acme", "demo", domain.CreateLabelInput{Name: "docs"}); !errors.Is(err, want) {
		t.Errorf("CreateLabel() error = %v, want %v", err, want)
	}
}

func TestCreateLabelValidation(t *testing.T) {
	svc := &stubLabelWriteService{}
	_, err := CreateLabel(context.Background(), svc, "acme", "demo", domain.CreateLabelInput{})
	if err == nil || !isValidation(err) {
		t.Errorf("expected validation error for empty label name, got %v", err)
	}
	if svc.calls != 0 {
		t.Errorf("service should not be called on validation failure")
	}
}

// =============================================================================
// forgejo_label_update — passthrough (idempotent, no validation)
// =============================================================================

func TestUpdateLabel(t *testing.T) {
	svc := &stubLabelWriteService{label: domain.Label{ID: 1, Name: "bugfix"}}
	got, err := UpdateLabel(context.Background(), svc, "acme", "demo", 1, domain.UpdateLabelInput{
		Name: "bugfix", Color: "000000",
	})
	if err != nil {
		t.Fatalf("UpdateLabel() error = %v", err)
	}
	if got.Name != "bugfix" {
		t.Errorf("label = %+v", got)
	}
	if svc.calls != 1 || svc.gotOwner != "acme" || svc.gotRepo != "demo" || svc.gotID != 1 {
		t.Errorf("args not forwarded: %+v", svc)
	}
	if svc.gotUpdateIn.Name != "bugfix" || svc.gotUpdateIn.Color != "000000" {
		t.Errorf("input not forwarded: %+v", svc.gotUpdateIn)
	}
}

func TestUpdateLabelPropagatesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindForbidden, "no permission")
	svc := &stubLabelWriteService{err: want}
	if _, err := UpdateLabel(context.Background(), svc, "acme", "demo", 1, domain.UpdateLabelInput{}); !errors.Is(err, want) {
		t.Errorf("UpdateLabel() error = %v, want %v", err, want)
	}
}

func TestUpdateLabelIdempotent(t *testing.T) {
	svc := &stubLabelWriteService{label: domain.Label{ID: 1}}
	in := domain.UpdateLabelInput{Name: "bug"}
	for i := 0; i < 2; i++ {
		if _, err := UpdateLabel(context.Background(), svc, "acme", "demo", 1, in); err != nil {
			t.Fatalf("UpdateLabel() iteration %d error = %v", i, err)
		}
	}
	if svc.calls != 2 {
		t.Errorf("expected 2 idempotent calls, got %d", svc.calls)
	}
}

// =============================================================================
// forgejo_label_delete — validation (id > 0)
// =============================================================================

func TestDeleteLabel(t *testing.T) {
	svc := &stubLabelDeleteService{}
	if err := DeleteLabel(context.Background(), svc, "acme", "demo", 1); err != nil {
		t.Fatalf("DeleteLabel() error = %v", err)
	}
	if svc.calls != 1 || svc.gotOwner != "acme" || svc.gotRepo != "demo" || svc.gotID != 1 {
		t.Errorf("args not forwarded: %+v", svc)
	}
}

func TestDeleteLabelPropagatesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindNotFound, "missing")
	svc := &stubLabelDeleteService{err: want}
	if err := DeleteLabel(context.Background(), svc, "acme", "demo", 1); !errors.Is(err, want) {
		t.Errorf("DeleteLabel() error = %v, want %v", err, want)
	}
}

func TestDeleteLabelValidation(t *testing.T) {
	svc := &stubLabelDeleteService{}
	if err := DeleteLabel(context.Background(), svc, "acme", "demo", 0); err == nil || !isValidation(err) {
		t.Errorf("expected validation error for non-positive id, got %v", err)
	}
	if svc.calls != 0 {
		t.Errorf("service should not be called on validation failure")
	}
}

// =============================================================================
// forgejo_issue_set_labels — validation (index > 0)
// =============================================================================

func TestSetIssueLabels(t *testing.T) {
	svc := &stubIssueLabelsService{}
	if err := SetIssueLabels(context.Background(), svc, "acme", "demo", 5, []int64{1, 2}); err != nil {
		t.Fatalf("SetIssueLabels() error = %v", err)
	}
	if svc.calls != 1 || svc.gotOwner != "acme" || svc.gotRepo != "demo" || svc.gotIndex != 5 {
		t.Errorf("args not forwarded: %+v", svc)
	}
	if len(svc.gotLabelIDs) != 2 || svc.gotLabelIDs[0] != 1 || svc.gotLabelIDs[1] != 2 {
		t.Errorf("label ids not forwarded: %+v", svc.gotLabelIDs)
	}
}

func TestSetIssueLabelsPropagatesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindForbidden, "no permission")
	svc := &stubIssueLabelsService{err: want}
	if err := SetIssueLabels(context.Background(), svc, "acme", "demo", 5, []int64{1}); !errors.Is(err, want) {
		t.Errorf("SetIssueLabels() error = %v, want %v", err, want)
	}
}

func TestSetIssueLabelsValidation(t *testing.T) {
	svc := &stubIssueLabelsService{}
	if err := SetIssueLabels(context.Background(), svc, "acme", "demo", 0, []int64{1}); err == nil || !isValidation(err) {
		t.Errorf("expected validation error for non-positive index, got %v", err)
	}
	if svc.calls != 0 {
		t.Errorf("service should not be called on validation failure")
	}
}
