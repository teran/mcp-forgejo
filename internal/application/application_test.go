package application

import (
	"context"
	"errors"
	"testing"

	"example.com/teran/mcp-forgejo/internal/domain"
)

type stubRepoService struct {
	repo    domain.Repository
	entries []domain.FileEntry
	file    domain.File
	err     error
}

func (s *stubRepoService) GetRepository(_ context.Context, _, _ string) (domain.Repository, error) {
	return s.repo, s.err
}

func (s *stubRepoService) ListContents(_ context.Context, _, _, _, _ string, _, _ int) ([]domain.FileEntry, error) {
	return s.entries, s.err
}

func (s *stubRepoService) GetFile(_ context.Context, _, _, _, _ string) (domain.File, error) {
	return s.file, s.err
}

type stubOrgService struct {
	orgs []domain.Organization
	err  error
}

func (s *stubOrgService) ListOrganizations(context.Context) ([]domain.Organization, error) {
	return s.orgs, s.err
}

type stubIssueService struct {
	issue    domain.Issue
	comments []domain.Comment
	issueErr error
	commErr  error
}

func (s *stubIssueService) GetIssue(_ context.Context, _, _ string, _ int64) (domain.Issue, error) {
	return s.issue, s.issueErr
}

func (s *stubIssueService) ListComments(_ context.Context, _, _ string, _ int64) ([]domain.Comment, error) {
	return s.comments, s.commErr
}

func TestGetRepository(t *testing.T) {
	svc := &stubRepoService{repo: domain.Repository{Name: "r"}}
	got, err := GetRepository(context.Background(), svc, "o", "r")
	if err != nil {
		t.Fatalf("GetRepository() error = %v", err)
	}
	if got.Name != "r" {
		t.Errorf("Name = %q", got.Name)
	}
}

func TestGetRepositoryError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindNotFound, "nope")
	svc := &stubRepoService{err: want}
	if _, err := GetRepository(context.Background(), svc, "o", "r"); !errors.Is(err, want) {
		t.Errorf("GetRepository() error = %v, want %v", err, want)
	}
}

func TestListContents(t *testing.T) {
	svc := &stubRepoService{entries: []domain.FileEntry{{Name: "a"}}}
	got, err := ListContents(context.Background(), svc, "o", "r", "src", "main", 1, 10)
	if err != nil {
		t.Fatalf("ListContents() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "a" {
		t.Errorf("got = %+v", got)
	}
}

func TestGetFile(t *testing.T) {
	svc := &stubRepoService{file: domain.File{Content: "hi", SHA: "s"}}
	got, err := GetFile(context.Background(), svc, "o", "r", "f", "")
	if err != nil {
		t.Fatalf("GetFile() error = %v", err)
	}
	if got.Content != "hi" || got.SHA != "s" {
		t.Errorf("got = %+v", got)
	}
}

func TestListOrganizations(t *testing.T) {
	svc := &stubOrgService{orgs: []domain.Organization{{Username: "acme"}}}
	got, err := ListOrganizations(context.Background(), svc)
	if err != nil {
		t.Fatalf("ListOrganizations() error = %v", err)
	}
	if len(got) != 1 || got[0].Username != "acme" {
		t.Errorf("got = %+v", got)
	}
}

func TestGetIssueComposesIssueAndComments(t *testing.T) {
	svc := &stubIssueService{
		issue:    domain.Issue{Number: 5, Title: "t"},
		comments: []domain.Comment{{ID: 1, Body: "c"}},
	}
	got, err := GetIssue(context.Background(), svc, "o", "r", 5)
	if err != nil {
		t.Fatalf("GetIssue() error = %v", err)
	}
	if got.Issue.Number != 5 || len(got.Comments) != 1 || got.Comments[0].Body != "c" {
		t.Errorf("got = %+v", got)
	}
}

func TestGetIssueErrorFromIssue(t *testing.T) {
	want := domain.NewForgejoError(domain.KindNotFound, "missing")
	svc := &stubIssueService{issueErr: want}
	if _, err := GetIssue(context.Background(), svc, "o", "r", 5); !errors.Is(err, want) {
		t.Errorf("GetIssue() error = %v, want %v", err, want)
	}
}

func TestGetIssueErrorFromComments(t *testing.T) {
	want := domain.NewForgejoError(domain.KindTransient, "boom")
	svc := &stubIssueService{issue: domain.Issue{Number: 5}, commErr: want}
	if _, err := GetIssue(context.Background(), svc, "o", "r", 5); !errors.Is(err, want) {
		t.Errorf("GetIssue() error = %v, want %v", err, want)
	}
}
