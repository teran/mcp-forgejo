package application

// Batch C delete use cases (SPEC 6.3 #26–#31). Test expectations for
// @developer: the application functions (orchestration + validation + guards)
// and the domain interfaces/types they must call. The stub services below
// implement the domain delete interfaces @developer must add to
// internal/domain.

import (
	"context"
	"errors"
	"testing"

	"example.com/teran/mcp-forgejo/internal/domain"
)

// --- stubs for the new domain delete interfaces -----------------------------

type stubFileDeleteService struct {
	file                               domain.File
	getErr                             error
	deleteRes                          domain.FileResult
	deleteErr                          error
	deleteInput                        domain.DeleteFileInput
	getCalls                           int
	deleteCalls                        int
	gotOwner, gotRepo, gotPath, gotRef string
}

func (s *stubFileDeleteService) GetFile(_ context.Context, owner, repo, path, ref string) (domain.File, error) {
	s.getCalls++
	s.gotOwner, s.gotRepo, s.gotPath, s.gotRef = owner, repo, path, ref
	return s.file, s.getErr
}

func (s *stubFileDeleteService) DeleteFile(_ context.Context, in domain.DeleteFileInput) (domain.FileResult, error) {
	s.deleteCalls++
	s.deleteInput = in
	return s.deleteRes, s.deleteErr
}

type stubBranchDeleteService struct {
	repo                         domain.Repository
	getErr                       error
	deleteErr                    error
	getCalls                     int
	deleteCalls                  int
	gotOwner, gotRepo, gotBranch string
}

func (s *stubBranchDeleteService) GetRepository(_ context.Context, owner, repo string) (domain.Repository, error) {
	s.getCalls++
	s.gotOwner, s.gotRepo = owner, repo
	return s.repo, s.getErr
}

func (s *stubBranchDeleteService) DeleteBranch(_ context.Context, owner, repo, branch string) error {
	s.deleteCalls++
	s.gotOwner, s.gotRepo, s.gotBranch = owner, repo, branch
	return s.deleteErr
}

type stubIssueDeleteService struct {
	err               error
	deleteCalls       int
	gotOwner, gotRepo string
	gotIndex          int64
}

func (s *stubIssueDeleteService) DeleteIssue(_ context.Context, owner, repo string, index int64) error {
	s.deleteCalls++
	s.gotOwner, s.gotRepo, s.gotIndex = owner, repo, index
	return s.err
}

type stubCommentDeleteService struct {
	err               error
	deleteCalls       int
	gotOwner, gotRepo string
	gotCommentID      int64
}

func (s *stubCommentDeleteService) DeleteComment(_ context.Context, owner, repo string, commentID int64) error {
	s.deleteCalls++
	s.gotOwner, s.gotRepo, s.gotCommentID = owner, repo, commentID
	return s.err
}

type stubReleaseDeleteService struct {
	err               error
	deleteCalls       int
	gotOwner, gotRepo string
	gotID             int64
}

func (s *stubReleaseDeleteService) DeleteRelease(_ context.Context, owner, repo string, id int64) error {
	s.deleteCalls++
	s.gotOwner, s.gotRepo, s.gotID = owner, repo, id
	return s.err
}

type stubRepositoryDeleteService struct {
	err               error
	deleteCalls       int
	gotOwner, gotRepo string
}

func (s *stubRepositoryDeleteService) DeleteRepository(_ context.Context, owner, repo string) error {
	s.deleteCalls++
	s.gotOwner, s.gotRepo = owner, repo
	return s.err
}

// =============================================================================
// #26 forgejo_file_delete
// =============================================================================

func TestDeleteFile(t *testing.T) {
	svc := &stubFileDeleteService{file: domain.File{SHA: "f1"}, deleteRes: domain.FileResult{SHA: "f1", CommitSHA: "c1"}}
	res, err := DeleteFile(context.Background(), svc, domain.DeleteFileInput{
		Owner: "acme", Repo: "demo", Path: "main.go", Branch: "main", Message: "remove",
	})
	if err != nil {
		t.Fatalf("DeleteFile() error = %v", err)
	}
	if svc.getCalls != 1 || svc.deleteCalls != 1 {
		t.Errorf("expected one GetFile + one DeleteFile, got get=%d delete=%d", svc.getCalls, svc.deleteCalls)
	}
	if svc.deleteInput.SHA != "f1" {
		t.Errorf("delete must carry the probed file sha, got %+v", svc.deleteInput)
	}
	if svc.deleteInput.Owner != "acme" || svc.deleteInput.Path != "main.go" || svc.deleteInput.Message != "remove" {
		t.Errorf("delete input = %+v", svc.deleteInput)
	}
	if res.CommitSHA != "c1" {
		t.Errorf("res = %+v", res)
	}
}

func TestDeleteFilePropagatesProbeError(t *testing.T) {
	getErr := domain.NewForgejoError(domain.KindNotFound, "missing")
	svc := &stubFileDeleteService{getErr: getErr}
	_, err := DeleteFile(context.Background(), svc, domain.DeleteFileInput{Owner: "o", Repo: "r", Path: "a"})
	if !errors.Is(err, getErr) {
		t.Errorf("DeleteFile() error = %v, want %v", err, getErr)
	}
	if svc.deleteCalls != 0 {
		t.Errorf("no delete should happen when the probe fails")
	}
}

func TestDeleteFilePropagatesDeleteError(t *testing.T) {
	delErr := domain.NewForgejoError(domain.KindTransient, "boom")
	svc := &stubFileDeleteService{file: domain.File{SHA: "f1"}, deleteErr: delErr}
	_, err := DeleteFile(context.Background(), svc, domain.DeleteFileInput{Owner: "o", Repo: "r", Path: "a"})
	if !errors.Is(err, delErr) {
		t.Errorf("DeleteFile() error = %v, want %v", err, delErr)
	}
}

func TestDeleteFileValidation(t *testing.T) {
	svc := &stubFileDeleteService{}
	_, err := DeleteFile(context.Background(), svc, domain.DeleteFileInput{Owner: "o", Repo: "r"})
	if err == nil || !isValidation(err) {
		t.Errorf("expected validation error for empty path, got %v", err)
	}
	if svc.getCalls != 0 {
		t.Errorf("service should not be called on validation failure")
	}
}

// =============================================================================
// #27 forgejo_branch_delete
// =============================================================================

func TestDeleteBranch(t *testing.T) {
	svc := &stubBranchDeleteService{repo: domain.Repository{DefaultBranch: "main"}}
	if err := DeleteBranch(context.Background(), svc, "acme", "demo", "dev"); err != nil {
		t.Fatalf("DeleteBranch() error = %v", err)
	}
	if svc.getCalls != 1 || svc.deleteCalls != 1 {
		t.Errorf("expected one GetRepository + one DeleteBranch, got get=%d delete=%d", svc.getCalls, svc.deleteCalls)
	}
	if svc.gotBranch != "dev" {
		t.Errorf("branch = %q, want dev", svc.gotBranch)
	}
}

func TestDeleteBranchRejectsDefault(t *testing.T) {
	svc := &stubBranchDeleteService{repo: domain.Repository{DefaultBranch: "main"}}
	err := DeleteBranch(context.Background(), svc, "acme", "demo", "main")
	if err == nil || !isValidation(err) {
		t.Errorf("expected validation error for default branch, got %v", err)
	}
	if svc.deleteCalls != 0 {
		t.Errorf("default branch must never be deleted")
	}
}

func TestDeleteBranchPropagatesGetRepoError(t *testing.T) {
	getErr := domain.NewForgejoError(domain.KindNotFound, "missing")
	svc := &stubBranchDeleteService{getErr: getErr}
	err := DeleteBranch(context.Background(), svc, "acme", "demo", "dev")
	if !errors.Is(err, getErr) {
		t.Errorf("DeleteBranch() error = %v, want %v", err, getErr)
	}
	if svc.deleteCalls != 0 {
		t.Errorf("no delete should happen when GetRepository fails")
	}
}

func TestDeleteBranchPropagatesDeleteError(t *testing.T) {
	delErr := domain.NewForgejoError(domain.KindForbidden, "protected")
	svc := &stubBranchDeleteService{repo: domain.Repository{DefaultBranch: "main"}, deleteErr: delErr}
	err := DeleteBranch(context.Background(), svc, "acme", "demo", "dev")
	if !errors.Is(err, delErr) {
		t.Errorf("DeleteBranch() error = %v, want %v", err, delErr)
	}
}

func TestDeleteBranchValidation(t *testing.T) {
	svc := &stubBranchDeleteService{repo: domain.Repository{DefaultBranch: "main"}}
	err := DeleteBranch(context.Background(), svc, "acme", "demo", "")
	if err == nil || !isValidation(err) {
		t.Errorf("expected validation error for empty branch, got %v", err)
	}
	if svc.getCalls != 0 {
		t.Errorf("service should not be called on validation failure")
	}
}

// =============================================================================
// #28 forgejo_issue_delete
// =============================================================================

func TestDeleteIssue(t *testing.T) {
	svc := &stubIssueDeleteService{}
	if err := DeleteIssue(context.Background(), svc, "acme", "demo", 7); err != nil {
		t.Fatalf("DeleteIssue() error = %v", err)
	}
	if svc.deleteCalls != 1 || svc.gotIndex != 7 {
		t.Errorf("deleteCalls=%d gotIndex=%d", svc.deleteCalls, svc.gotIndex)
	}
}

func TestDeleteIssuePropagatesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindNotFound, "missing")
	svc := &stubIssueDeleteService{err: want}
	if err := DeleteIssue(context.Background(), svc, "acme", "demo", 7); !errors.Is(err, want) {
		t.Errorf("DeleteIssue() error = %v, want %v", err, want)
	}
}

func TestDeleteIssueValidation(t *testing.T) {
	svc := &stubIssueDeleteService{}
	if err := DeleteIssue(context.Background(), svc, "acme", "demo", 0); err == nil || !isValidation(err) {
		t.Errorf("expected validation error for index 0, got %v", err)
	}
	if svc.deleteCalls != 0 {
		t.Errorf("service should not be called on validation failure")
	}
}

// =============================================================================
// #29 forgejo_comment_delete
// =============================================================================

func TestDeleteComment(t *testing.T) {
	svc := &stubCommentDeleteService{}
	if err := DeleteComment(context.Background(), svc, "acme", "demo", 9); err != nil {
		t.Fatalf("DeleteComment() error = %v", err)
	}
	if svc.deleteCalls != 1 || svc.gotCommentID != 9 {
		t.Errorf("deleteCalls=%d gotCommentID=%d", svc.deleteCalls, svc.gotCommentID)
	}
}

func TestDeleteCommentPropagatesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindNotFound, "missing")
	svc := &stubCommentDeleteService{err: want}
	if err := DeleteComment(context.Background(), svc, "acme", "demo", 9); !errors.Is(err, want) {
		t.Errorf("DeleteComment() error = %v, want %v", err, want)
	}
}

func TestDeleteCommentValidation(t *testing.T) {
	svc := &stubCommentDeleteService{}
	if err := DeleteComment(context.Background(), svc, "acme", "demo", 0); err == nil || !isValidation(err) {
		t.Errorf("expected validation error for comment id 0, got %v", err)
	}
	if svc.deleteCalls != 0 {
		t.Errorf("service should not be called on validation failure")
	}
}

// =============================================================================
// #30 forgejo_release_delete
// =============================================================================

func TestDeleteRelease(t *testing.T) {
	svc := &stubReleaseDeleteService{}
	if err := DeleteRelease(context.Background(), svc, "acme", "demo", 3); err != nil {
		t.Fatalf("DeleteRelease() error = %v", err)
	}
	if svc.deleteCalls != 1 || svc.gotID != 3 {
		t.Errorf("deleteCalls=%d gotID=%d", svc.deleteCalls, svc.gotID)
	}
}

func TestDeleteReleasePropagatesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindNotFound, "missing")
	svc := &stubReleaseDeleteService{err: want}
	if err := DeleteRelease(context.Background(), svc, "acme", "demo", 3); !errors.Is(err, want) {
		t.Errorf("DeleteRelease() error = %v, want %v", err, want)
	}
}

func TestDeleteReleaseValidation(t *testing.T) {
	svc := &stubReleaseDeleteService{}
	if err := DeleteRelease(context.Background(), svc, "acme", "demo", 0); err == nil || !isValidation(err) {
		t.Errorf("expected validation error for id 0, got %v", err)
	}
	if svc.deleteCalls != 0 {
		t.Errorf("service should not be called on validation failure")
	}
}

// =============================================================================
// #31 forgejo_repo_delete — requires explicit confirmation
// =============================================================================

func TestDeleteRepositoryConfirmed(t *testing.T) {
	svc := &stubRepositoryDeleteService{}
	if err := DeleteRepository(context.Background(), svc, "acme", "demo", true); err != nil {
		t.Fatalf("DeleteRepository() error = %v", err)
	}
	if svc.deleteCalls != 1 || svc.gotOwner != "acme" || svc.gotRepo != "demo" {
		t.Errorf("deleteCalls=%d got=%s/%s", svc.deleteCalls, svc.gotOwner, svc.gotRepo)
	}
}

func TestDeleteRepositoryRequiresConfirmation(t *testing.T) {
	svc := &stubRepositoryDeleteService{}
	err := DeleteRepository(context.Background(), svc, "acme", "demo", false)
	if err == nil || !isValidation(err) {
		t.Errorf("expected validation error without confirmation, got %v", err)
	}
	if svc.deleteCalls != 0 {
		t.Errorf("repository must never be deleted without explicit confirmation")
	}
}

func TestDeleteRepositoryPropagatesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindForbidden, "no permission")
	svc := &stubRepositoryDeleteService{err: want}
	if err := DeleteRepository(context.Background(), svc, "acme", "demo", true); !errors.Is(err, want) {
		t.Errorf("DeleteRepository() error = %v, want %v", err, want)
	}
}
