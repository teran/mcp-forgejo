package application

// Batch T use cases (SPEC §6): forgejo_tag_list, forgejo_tag_create,
// forgejo_tag_delete, forgejo_repo_fork, forgejo_repo_update. Test expectations
// for @developer: the application functions (orchestration + validation) and
// the domain interfaces/types they must call. The stub services below
// implement the domain interfaces @developer must add to internal/domain.

import (
	"context"
	"errors"
	"testing"

	"github.com/teran/mcp-forgejo/internal/domain"
)

// --- stubs for the new domain interfaces ------------------------------------

type stubTagService struct {
	tags              []domain.Tag
	err               error
	calls             int
	gotOwner, gotRepo string
}

func (s *stubTagService) ListTags(_ context.Context, owner, repo string) ([]domain.Tag, error) {
	s.calls++
	s.gotOwner, s.gotRepo = owner, repo
	return s.tags, s.err
}

type stubTagWriteService struct {
	tag               domain.Tag
	err               error
	calls             int
	gotOwner, gotRepo string
	gotIn             domain.CreateTagInput
}

func (s *stubTagWriteService) CreateTag(_ context.Context, owner, repo string, in domain.CreateTagInput) (domain.Tag, error) {
	s.calls++
	s.gotOwner, s.gotRepo = owner, repo
	s.gotIn = in
	return s.tag, s.err
}

type stubTagDeleteService struct {
	err                       error
	calls                     int
	gotOwner, gotRepo, gotTag string
}

func (s *stubTagDeleteService) DeleteTag(_ context.Context, owner, repo, tag string) error {
	s.calls++
	s.gotOwner, s.gotRepo, s.gotTag = owner, repo, tag
	return s.err
}

type stubRepoForkService struct {
	repo              domain.Repository
	err               error
	calls             int
	gotOwner, gotRepo string
	gotIn             domain.ForkRepositoryInput
}

func (s *stubRepoForkService) ForkRepository(_ context.Context, owner, repo string, in domain.ForkRepositoryInput) (domain.Repository, error) {
	s.calls++
	s.gotOwner, s.gotRepo = owner, repo
	s.gotIn = in
	return s.repo, s.err
}

type stubRepoUpdateService struct {
	repo              domain.Repository
	err               error
	calls             int
	gotOwner, gotRepo string
	gotIn             domain.UpdateRepositoryInput
}

func (s *stubRepoUpdateService) UpdateRepository(_ context.Context, owner, repo string, in domain.UpdateRepositoryInput) (domain.Repository, error) {
	s.calls++
	s.gotOwner, s.gotRepo = owner, repo
	s.gotIn = in
	return s.repo, s.err
}

// =============================================================================
// forgejo_tag_list — passthrough
// =============================================================================

func TestListTags(t *testing.T) {
	svc := &stubTagService{tags: []domain.Tag{{Name: "v1.0", SHA: "abc"}}}
	got, err := ListTags(context.Background(), svc, "acme", "demo")
	if err != nil {
		t.Fatalf("ListTags() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "v1.0" || got[0].SHA != "abc" {
		t.Errorf("tags = %+v", got)
	}
	if svc.calls != 1 || svc.gotOwner != "acme" || svc.gotRepo != "demo" {
		t.Errorf("args not forwarded: %+v", svc)
	}
}

func TestListTagsPropagatesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindTransient, "boom")
	svc := &stubTagService{err: want}
	if _, err := ListTags(context.Background(), svc, "acme", "demo"); !errors.Is(err, want) {
		t.Errorf("ListTags() error = %v, want %v", err, want)
	}
}

// =============================================================================
// forgejo_tag_create — validation (name required)
// =============================================================================

func TestCreateTag(t *testing.T) {
	svc := &stubTagWriteService{tag: domain.Tag{Name: "v1.0", SHA: "abc"}}
	got, err := CreateTag(context.Background(), svc, "acme", "demo", domain.CreateTagInput{
		Name: "v1.0", Target: "main", Message: "release 1",
	})
	if err != nil {
		t.Fatalf("CreateTag() error = %v", err)
	}
	if got.Name != "v1.0" {
		t.Errorf("tag = %+v", got)
	}
	if svc.calls != 1 || svc.gotOwner != "acme" || svc.gotRepo != "demo" {
		t.Errorf("args not forwarded: %+v", svc)
	}
	if svc.gotIn.Name != "v1.0" || svc.gotIn.Target != "main" || svc.gotIn.Message != "release 1" {
		t.Errorf("input not forwarded: %+v", svc.gotIn)
	}
}

func TestCreateTagPropagatesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindConflict, "exists")
	svc := &stubTagWriteService{err: want}
	if _, err := CreateTag(context.Background(), svc, "acme", "demo", domain.CreateTagInput{Name: "v1.0"}); !errors.Is(err, want) {
		t.Errorf("CreateTag() error = %v, want %v", err, want)
	}
}

func TestCreateTagValidation(t *testing.T) {
	svc := &stubTagWriteService{}
	_, err := CreateTag(context.Background(), svc, "acme", "demo", domain.CreateTagInput{})
	if err == nil || !isValidation(err) {
		t.Errorf("expected validation error for empty tag name, got %v", err)
	}
	if svc.calls != 0 {
		t.Errorf("service should not be called on validation failure")
	}
}

// =============================================================================
// forgejo_tag_delete — validation (tag required)
// =============================================================================

func TestDeleteTag(t *testing.T) {
	svc := &stubTagDeleteService{}
	if err := DeleteTag(context.Background(), svc, "acme", "demo", "v1.0"); err != nil {
		t.Fatalf("DeleteTag() error = %v", err)
	}
	if svc.calls != 1 || svc.gotOwner != "acme" || svc.gotRepo != "demo" || svc.gotTag != "v1.0" {
		t.Errorf("args not forwarded: %+v", svc)
	}
}

func TestDeleteTagPropagatesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindNotFound, "missing")
	svc := &stubTagDeleteService{err: want}
	if err := DeleteTag(context.Background(), svc, "acme", "demo", "v1.0"); !errors.Is(err, want) {
		t.Errorf("DeleteTag() error = %v, want %v", err, want)
	}
}

func TestDeleteTagValidation(t *testing.T) {
	svc := &stubTagDeleteService{}
	if err := DeleteTag(context.Background(), svc, "acme", "demo", ""); err == nil || !isValidation(err) {
		t.Errorf("expected validation error for empty tag, got %v", err)
	}
	if svc.calls != 0 {
		t.Errorf("service should not be called on validation failure")
	}
}

// =============================================================================
// forgejo_repo_fork — validation (owner/repo required)
// =============================================================================

func TestForkRepository(t *testing.T) {
	svc := &stubRepoForkService{repo: domain.Repository{FullName: "acme/demo-fork"}}
	got, err := ForkRepository(context.Background(), svc, "acme", "demo", domain.ForkRepositoryInput{
		Organization: "team", Name: "demo-fork", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("ForkRepository() error = %v", err)
	}
	if got.FullName != "acme/demo-fork" {
		t.Errorf("repo = %+v", got)
	}
	if svc.calls != 1 || svc.gotOwner != "acme" || svc.gotRepo != "demo" {
		t.Errorf("args not forwarded: %+v", svc)
	}
	if svc.gotIn.Name != "demo-fork" || svc.gotIn.DefaultBranch != "main" {
		t.Errorf("input not forwarded: %+v", svc.gotIn)
	}
}

func TestForkRepositoryPropagatesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindConflict, "exists")
	svc := &stubRepoForkService{err: want}
	if _, err := ForkRepository(context.Background(), svc, "acme", "demo", domain.ForkRepositoryInput{Name: "f"}); !errors.Is(err, want) {
		t.Errorf("ForkRepository() error = %v, want %v", err, want)
	}
}

func TestForkRepositoryValidation(t *testing.T) {
	svc := &stubRepoForkService{}
	if _, err := ForkRepository(context.Background(), svc, "", "demo", domain.ForkRepositoryInput{Name: "f"}); err == nil || !isValidation(err) {
		t.Errorf("expected validation error for empty owner, got %v", err)
	}
	if _, err := ForkRepository(context.Background(), svc, "acme", "", domain.ForkRepositoryInput{Name: "f"}); err == nil || !isValidation(err) {
		t.Errorf("expected validation error for empty repo, got %v", err)
	}
	if svc.calls != 0 {
		t.Errorf("service should not be called on validation failure")
	}
}

// =============================================================================
// forgejo_repo_update — passthrough (idempotent, no validation)
// =============================================================================

func TestUpdateRepository(t *testing.T) {
	svc := &stubRepoUpdateService{repo: domain.Repository{Description: "new desc"}}
	priv := true
	got, err := UpdateRepository(context.Background(), svc, "acme", "demo", domain.UpdateRepositoryInput{
		Description: "new desc", Private: &priv,
	})
	if err != nil {
		t.Fatalf("UpdateRepository() error = %v", err)
	}
	if got.Description != "new desc" {
		t.Errorf("repo = %+v", got)
	}
	if svc.calls != 1 || svc.gotOwner != "acme" || svc.gotRepo != "demo" {
		t.Errorf("args not forwarded: %+v", svc)
	}
	if svc.gotIn.Description != "new desc" || svc.gotIn.Private == nil || !*svc.gotIn.Private {
		t.Errorf("input not forwarded: %+v", svc.gotIn)
	}
}

func TestUpdateRepositoryPropagatesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindForbidden, "no permission")
	svc := &stubRepoUpdateService{err: want}
	if _, err := UpdateRepository(context.Background(), svc, "acme", "demo", domain.UpdateRepositoryInput{}); !errors.Is(err, want) {
		t.Errorf("UpdateRepository() error = %v, want %v", err, want)
	}
}

// UpdateRepository is idempotent: repeating the same edit has no extra effect,
// but the service is still invoked each time (like issue_update).
func TestUpdateRepositoryIdempotent(t *testing.T) {
	svc := &stubRepoUpdateService{repo: domain.Repository{Description: "d"}}
	in := domain.UpdateRepositoryInput{Description: "d"}
	for i := 0; i < 2; i++ {
		if _, err := UpdateRepository(context.Background(), svc, "acme", "demo", in); err != nil {
			t.Fatalf("UpdateRepository() iteration %d error = %v", i, err)
		}
	}
	if svc.calls != 2 {
		t.Errorf("expected 2 idempotent calls, got %d", svc.calls)
	}
}
