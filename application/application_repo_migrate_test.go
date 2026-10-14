package application

// forgejo_repo_migrate (SPEC 6.2, repoMigrate): migrates a remote git
// repository (e.g. from GitHub) into Forgejo. It is a WRITE/UPDATE tool that is
// NOT idempotent (a second migrate of the same name conflicts). Test
// expectations for @developer: the application function (orchestration +
// validation) and the domain interface/type it must call.
//
// @developer must add to domain:
//   - type MigrateRepositoryInput struct with json tags clone_addr / repo_name
//     (required) and optional repo_owner, service, auth_username, auth_token,
//     mirror, mirror_interval, private, description, issues, labels,
//     milestones, pull_requests, releases, wiki, lfs, lfs_endpoint.
//     Optional booleans are *bool so an omitted value lets Forgejo apply its
//     server-side default (mirrors UpdateRepositoryInput.Private).
//   - type RepositoryMigrateService interface { MigrateRepository(ctx, in
//     MigrateRepositoryInput) (Repository, error) }

import (
	"context"
	"errors"
	"testing"

	"github.com/teran/mcp-forgejo/domain"
)

// stubRepoMigrateService implements domain.RepositoryMigrateService.
type stubRepoMigrateService struct {
	repo  domain.Repository
	err   error
	calls int
	gotIn domain.MigrateRepositoryInput
}

func (s *stubRepoMigrateService) MigrateRepository(_ context.Context, in domain.MigrateRepositoryInput) (domain.Repository, error) {
	s.calls++
	s.gotIn = in
	return s.repo, s.err
}

// =============================================================================
// forgejo_repo_migrate — happy path + forwarding
// =============================================================================

func TestMigrateRepository(t *testing.T) {
	svc := &stubRepoMigrateService{repo: domain.Repository{Name: "migrated", FullName: "alice/migrated"}}

	priv, mirror, issues := true, true, true
	got, err := MigrateRepository(context.Background(), svc, domain.MigrateRepositoryInput{
		CloneAddr:    "https://github.com/acme/upstream.git",
		RepoName:     "migrated",
		RepoOwner:    "alice",
		Service:      "github",
		AuthUsername: "alice",
		AuthToken:    "ghp_src_secret",
		Mirror:       &mirror,
		Private:      &priv,
		Description:  "imported repo",
		Issues:       &issues,
	})
	if err != nil {
		t.Fatalf("MigrateRepository() error = %v", err)
	}
	if got.Name != "migrated" || got.FullName != "alice/migrated" {
		t.Errorf("repo = %+v", got)
	}
	if svc.calls != 1 {
		t.Errorf("expected one MigrateRepository call, got %d", svc.calls)
	}
	if svc.gotIn.CloneAddr != "https://github.com/acme/upstream.git" ||
		svc.gotIn.RepoName != "migrated" || svc.gotIn.RepoOwner != "alice" ||
		svc.gotIn.Service != "github" || svc.gotIn.AuthUsername != "alice" ||
		svc.gotIn.AuthToken != "ghp_src_secret" || svc.gotIn.Description != "imported repo" {
		t.Errorf("input not forwarded: %+v", svc.gotIn)
	}
	if svc.gotIn.Mirror == nil || !*svc.gotIn.Mirror || svc.gotIn.Private == nil || !*svc.gotIn.Private || svc.gotIn.Issues == nil || !*svc.gotIn.Issues {
		t.Errorf("pointer booleans not forwarded: %+v", svc.gotIn)
	}
}

func TestMigrateRepositoryPropagatesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindConflict, "repository name already exists")
	svc := &stubRepoMigrateService{err: want}
	_, err := MigrateRepository(context.Background(), svc, domain.MigrateRepositoryInput{
		CloneAddr: "https://github.com/acme/upstream.git", RepoName: "migrated",
	})
	if !errors.Is(err, want) {
		t.Errorf("MigrateRepository() error = %v, want %v", err, want)
	}
}

// =============================================================================
// forgejo_repo_migrate — required-field validation (before any service call)
// =============================================================================

func TestMigrateRepositoryValidationCloneAddr(t *testing.T) {
	svc := &stubRepoMigrateService{}
	_, err := MigrateRepository(context.Background(), svc, domain.MigrateRepositoryInput{RepoName: "migrated"})
	if err == nil || !isValidation(err) {
		t.Errorf("expected validation error for empty clone_addr, got %v", err)
	}
	if svc.calls != 0 {
		t.Errorf("service should not be called on validation failure")
	}
}

func TestMigrateRepositoryValidationRepoName(t *testing.T) {
	svc := &stubRepoMigrateService{}
	_, err := MigrateRepository(context.Background(), svc, domain.MigrateRepositoryInput{CloneAddr: "https://github.com/acme/upstream.git"})
	if err == nil || !isValidation(err) {
		t.Errorf("expected validation error for empty repo_name, got %v", err)
	}
	if svc.calls != 0 {
		t.Errorf("service should not be called on validation failure")
	}
}

// The `service` field is an enum; an unknown value must be rejected even though
// clone_addr/repo_name are present. A valid value passes.
func TestMigrateRepositoryValidationService(t *testing.T) {
	svc := &stubRepoMigrateService{}
	_, err := MigrateRepository(context.Background(), svc, domain.MigrateRepositoryInput{
		CloneAddr: "https://github.com/acme/upstream.git", RepoName: "migrated", Service: "bitbucket",
	})
	if err == nil || !isValidation(err) {
		t.Errorf("expected validation error for unsupported service, got %v", err)
	}
	if svc.calls != 0 {
		t.Errorf("service should not be called on validation failure")
	}

	// Every documented service value is accepted and forwarded.
	svc2 := &stubRepoMigrateService{repo: domain.Repository{Name: "migrated"}}
	for _, svcName := range []string{"github", "git", "gitea", "gitlab", "gogs", "onedev", "gitbucket", "codebase", "forgejo", "pagure"} {
		_, err := MigrateRepository(context.Background(), svc2, domain.MigrateRepositoryInput{
			CloneAddr: "https://github.com/acme/upstream.git", RepoName: "migrated", Service: svcName,
		})
		if err != nil {
			t.Errorf("MigrateRepository(service=%q) error = %v", svcName, err)
		}
	}
	// An empty service is allowed (Forgejo defaults to github).
	if _, err := MigrateRepository(context.Background(), svc2, domain.MigrateRepositoryInput{
		CloneAddr: "https://github.com/acme/upstream.git", RepoName: "migrated",
	}); err != nil {
		t.Errorf("MigrateRepository(empty service) error = %v", err)
	}
}

// =============================================================================
// forgejo_repo_migrate — not idempotent
// =============================================================================

func TestMigrateRepositoryNotIdempotent(t *testing.T) {
	svc := &stubRepoMigrateService{repo: domain.Repository{Name: "migrated"}}
	in := domain.MigrateRepositoryInput{CloneAddr: "https://github.com/acme/upstream.git", RepoName: "migrated"}
	for i := 0; i < 2; i++ {
		if _, err := MigrateRepository(context.Background(), svc, in); err != nil {
			t.Fatalf("MigrateRepository() iteration %d error = %v", i, err)
		}
	}
	// Each migrate is a fresh write on the service (a second migrate of the
	// same name conflicts), so the service is invoked every time.
	if svc.calls != 2 {
		t.Errorf("expected 2 migrate calls, got %d", svc.calls)
	}
}
