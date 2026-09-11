package application

// Batch B write/update use cases (SPEC 6.2 #14–#25). Test expectations for
// @developer: the application functions (orchestration + validation) and the
// domain interfaces/types they must call. The stub services below implement
// the domain write interfaces @developer must add to internal/domain.

import (
	"context"
	"errors"
	"testing"

	"example.com/teran/mcp-forgejo/internal/domain"
)

// --- stubs for the new domain write interfaces ------------------------------

type stubRepoWriteService struct {
	repo        domain.Repository
	createErr   error
	gotIn       domain.CreateRepositoryInput
	createCalls int
}

func (s *stubRepoWriteService) CreateRepository(_ context.Context, in domain.CreateRepositoryInput) (domain.Repository, error) {
	s.createCalls++
	s.gotIn = in
	return s.repo, s.createErr
}

type stubFileWriteOrchestrator struct {
	file            domain.File
	getErr          error
	createRes       domain.FileResult
	createErr       error
	updateRes       domain.FileResult
	updateErr       error
	createInput     domain.CreateFileInput
	updateInput     domain.UpdateFileInput
	createCalls     int
	updateCalls     int
	changeRes       domain.ChangeFilesResult
	changeErr       error
	changeInput     domain.ChangeFilesInput
	changeCalls     int
	gotPath, gotRef string
}

func (s *stubFileWriteOrchestrator) GetFile(_ context.Context, _, _, path, ref string) (domain.File, error) {
	s.gotPath, s.gotRef = path, ref
	return s.file, s.getErr
}

func (s *stubFileWriteOrchestrator) CreateFile(_ context.Context, in domain.CreateFileInput) (domain.FileResult, error) {
	s.createCalls++
	s.createInput = in
	return s.createRes, s.createErr
}

func (s *stubFileWriteOrchestrator) UpdateFile(_ context.Context, in domain.UpdateFileInput) (domain.FileResult, error) {
	s.updateCalls++
	s.updateInput = in
	return s.updateRes, s.updateErr
}

func (s *stubFileWriteOrchestrator) ChangeFiles(_ context.Context, in domain.ChangeFilesInput) (domain.ChangeFilesResult, error) {
	s.changeCalls++
	s.changeInput = in
	return s.changeRes, s.changeErr
}

type stubBranchWriteService struct {
	branch                            domain.Branch
	err                               error
	gotOwner, gotRepo, gotNew, gotOld string
}

func (s *stubBranchWriteService) CreateBranch(_ context.Context, owner, repo, newBranch, oldRef string) (domain.Branch, error) {
	s.gotOwner, s.gotRepo, s.gotNew, s.gotOld = owner, repo, newBranch, oldRef
	return s.branch, s.err
}

type stubIssueWriteService struct {
	issue                      domain.Issue
	comment                    domain.Comment
	err                        error
	gotIn                      domain.CreateIssueInput
	gotUpd                     domain.UpdateIssueInput
	createCalls                int
	updateCalls                int
	commentCalls               int
	gotOwner, gotRepo, gotBody string
	gotIndex                   int64
}

func (s *stubIssueWriteService) CreateIssue(_ context.Context, in domain.CreateIssueInput) (domain.Issue, error) {
	s.createCalls++
	s.gotIn = in
	return s.issue, s.err
}

func (s *stubIssueWriteService) UpdateIssue(_ context.Context, in domain.UpdateIssueInput) (domain.Issue, error) {
	s.updateCalls++
	s.gotUpd = in
	return s.issue, s.err
}

func (s *stubIssueWriteService) CreateIssueComment(_ context.Context, owner, repo string, index int64, body string) (domain.Comment, error) {
	s.commentCalls++
	s.gotOwner, s.gotRepo, s.gotIndex, s.gotBody = owner, repo, index, body
	return s.comment, s.err
}

type stubPullWriteService struct {
	pr                                              domain.PullRequest
	review                                          domain.Review
	result                                          domain.PullMergeResult
	err                                             error
	gotCreate                                       domain.CreatePullRequestInput
	gotUpd                                          domain.UpdatePullRequestInput
	gotOwner, gotRepo, gotMethod, gotBody, gotEvent string
	gotIndex                                        int64
	gotReviewID                                     int64
	merged                                          bool
	mergeErr                                        error
	mergeCalls                                      int
	createCalls                                     int
	updateCalls                                     int
	reviewCreateCalls                               int
	reviewSubmitCalls                               int
}

func (s *stubPullWriteService) CreatePullRequest(_ context.Context, in domain.CreatePullRequestInput) (domain.PullRequest, error) {
	s.createCalls++
	s.gotCreate = in
	return s.pr, s.err
}

func (s *stubPullWriteService) UpdatePullRequest(_ context.Context, in domain.UpdatePullRequestInput) (domain.PullRequest, error) {
	s.updateCalls++
	s.gotUpd = in
	return s.pr, s.err
}

func (s *stubPullWriteService) IsPullRequestMerged(_ context.Context, _, _ string, _ int64) (bool, error) {
	return s.merged, s.err
}

func (s *stubPullWriteService) MergePullRequest(_ context.Context, owner, repo string, index int64, method string) error {
	s.mergeCalls++
	s.gotOwner, s.gotRepo, s.gotIndex, s.gotMethod = owner, repo, index, method
	return s.mergeErr
}

func (s *stubPullWriteService) CreatePullReview(_ context.Context, _, _ string, _ int64, body string) (domain.Review, error) {
	s.reviewCreateCalls++
	s.gotBody = body
	return domain.Review{ID: 11, State: "PENDING"}, nil
}

func (s *stubPullWriteService) SubmitPullReview(_ context.Context, _, _ string, _ int64, reviewID int64, event string) (domain.Review, error) {
	s.reviewSubmitCalls++
	s.gotReviewID = reviewID
	s.gotEvent = event
	return domain.Review{ID: 11, State: event}, nil
}

type stubReleaseWriteService struct {
	release domain.Release
	err     error
	gotIn   domain.CreateReleaseInput
}

func (s *stubReleaseWriteService) CreateRelease(_ context.Context, in domain.CreateReleaseInput) (domain.Release, error) {
	s.gotIn = in
	return s.release, s.err
}

// isValidation returns true if err is a ForgejoError of kind validation.
func isValidation(err error) bool {
	var fe *domain.ForgejoError
	return errors.As(err, &fe) && fe.Kind == domain.KindValidation
}

// =============================================================================
// #14 forgejo_repo_create
// =============================================================================

func TestCreateRepository(t *testing.T) {
	svc := &stubRepoWriteService{repo: domain.Repository{Name: "demo"}}
	got, err := CreateRepository(context.Background(), svc, domain.CreateRepositoryInput{Name: "demo", Private: true, AutoInit: true})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}
	if got.Name != "demo" {
		t.Errorf("repo = %+v", got)
	}
	if svc.gotIn.Name != "demo" || !svc.gotIn.Private || !svc.gotIn.AutoInit {
		t.Errorf("input not forwarded: %+v", svc.gotIn)
	}
}

func TestCreateRepositoryError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindConflict, "exists")
	svc := &stubRepoWriteService{createErr: want}
	if _, err := CreateRepository(context.Background(), svc, domain.CreateRepositoryInput{Name: "demo"}); !errors.Is(err, want) {
		t.Errorf("CreateRepository() error = %v, want %v", err, want)
	}
}

func TestCreateRepositoryValidation(t *testing.T) {
	svc := &stubRepoWriteService{}
	_, err := CreateRepository(context.Background(), svc, domain.CreateRepositoryInput{Name: ""})
	if err == nil || !isValidation(err) {
		t.Errorf("expected validation error for empty name, got %v", err)
	}
	if svc.createCalls != 0 {
		t.Errorf("service should not be called on validation failure")
	}
}

// =============================================================================
// #15 forgejo_file_write — create/update dispatch (not idempotent)
// =============================================================================

func TestWriteFileCreatesWhenAbsent(t *testing.T) {
	// GetFile returns a not-found error => the use case must CreateFile.
	getErr := domain.NewForgejoError(domain.KindNotFound, "missing")
	svc := &stubFileWriteOrchestrator{getErr: getErr, createRes: domain.FileResult{SHA: "f1"}}
	res, err := WriteFile(context.Background(), svc, domain.WriteFileInput{
		Owner: "o", Repo: "r", Path: "a.txt", Branch: "main", Message: "add", Content: "hi",
	})
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if svc.createCalls != 1 || svc.updateCalls != 0 {
		t.Errorf("expected create, createCalls=%d updateCalls=%d", svc.createCalls, svc.updateCalls)
	}
	if svc.createInput.Path != "a.txt" || svc.createInput.Content != "hi" {
		t.Errorf("create input = %+v", svc.createInput)
	}
	if res.SHA != "f1" {
		t.Errorf("res = %+v", res)
	}
}

func TestWriteFileUpdatesWhenPresent(t *testing.T) {
	svc := &stubFileWriteOrchestrator{
		file:      domain.File{SHA: "existing-sha"},
		updateRes: domain.FileResult{SHA: "f2"},
	}
	res, err := WriteFile(context.Background(), svc, domain.WriteFileInput{
		Owner: "o", Repo: "r", Path: "a.txt", Branch: "main", Message: "upd", Content: "hi",
	})
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if svc.createCalls != 0 || svc.updateCalls != 1 {
		t.Errorf("expected update, createCalls=%d updateCalls=%d", svc.createCalls, svc.updateCalls)
	}
	if svc.updateInput.SHA != "existing-sha" {
		t.Errorf("update must carry the existing blob sha, got %+v", svc.updateInput)
	}
	if res.SHA != "f2" {
		t.Errorf("res = %+v", res)
	}
}

func TestWriteFilePropagatesNonNotFoundGetError(t *testing.T) {
	getErr := domain.NewForgejoError(domain.KindTransient, "boom")
	svc := &stubFileWriteOrchestrator{getErr: getErr}
	_, err := WriteFile(context.Background(), svc, domain.WriteFileInput{Owner: "o", Repo: "r", Path: "a", Message: "m", Content: "c"})
	if !errors.Is(err, getErr) {
		t.Errorf("WriteFile() error = %v, want %v", err, getErr)
	}
	if svc.createCalls != 0 || svc.updateCalls != 0 {
		t.Errorf("no write should happen on non-404 get error")
	}
}

func TestWriteFileValidation(t *testing.T) {
	svc := &stubFileWriteOrchestrator{}
	_, err := WriteFile(context.Background(), svc, domain.WriteFileInput{Owner: "o", Repo: "r", Message: "m", Content: "c"})
	if err == nil || !isValidation(err) {
		t.Errorf("expected validation error for empty path, got %v", err)
	}
}

// Not idempotent: each call records a new commit (a fresh write on the service)
// even for identical content.
func TestWriteFileNotIdempotent(t *testing.T) {
	svc := &stubFileWriteOrchestrator{
		file:      domain.File{SHA: "s"},
		updateRes: domain.FileResult{SHA: "f"},
	}
	in := domain.WriteFileInput{Owner: "o", Repo: "r", Path: "a", Branch: "main", Message: "m", Content: "c"}
	for i := 0; i < 2; i++ {
		if _, err := WriteFile(context.Background(), svc, in); err != nil {
			t.Fatalf("WriteFile() iteration %d error = %v", i, err)
		}
	}
	if svc.updateCalls != 2 {
		t.Errorf("expected 2 update calls (one new commit each), got %d", svc.updateCalls)
	}
}

// =============================================================================
// #16 forgejo_file_write_many
// =============================================================================

func TestWriteManyFiles(t *testing.T) {
	svc := &stubFileWriteOrchestrator{changeRes: domain.ChangeFilesResult{CommitSHA: "c9"}}
	res, err := WriteManyFiles(context.Background(), svc, domain.ChangeFilesInput{
		Owner: "o", Repo: "r", Branch: "main", Message: "add",
		Files: []domain.ChangeFileEntry{{Path: "a", Content: "x", Operation: "create"}},
	})
	if err != nil {
		t.Fatalf("WriteManyFiles() error = %v", err)
	}
	if svc.changeCalls != 1 {
		t.Errorf("changeCalls = %d, want 1", svc.changeCalls)
	}
	if svc.changeInput.Branch != "main" || len(svc.changeInput.Files) != 1 {
		t.Errorf("change input = %+v", svc.changeInput)
	}
	if res.CommitSHA != "c9" {
		t.Errorf("res = %+v", res)
	}
}

func TestWriteManyFilesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindTransient, "boom")
	svc := &stubFileWriteOrchestrator{changeErr: want}
	if _, err := WriteManyFiles(context.Background(), svc, domain.ChangeFilesInput{Owner: "o", Repo: "r"}); !errors.Is(err, want) {
		t.Errorf("WriteManyFiles() error = %v, want %v", err, want)
	}
}

// =============================================================================
// #17 forgejo_branch_create
// =============================================================================

func TestCreateBranch(t *testing.T) {
	svc := &stubBranchWriteService{branch: domain.Branch{Name: "dev", CommitSHA: "c1"}}
	got, err := CreateBranch(context.Background(), svc, "o", "r", "dev", "main")
	if err != nil {
		t.Fatalf("CreateBranch() error = %v", err)
	}
	if got.Name != "dev" {
		t.Errorf("branch = %+v", got)
	}
	if svc.gotOwner != "o" || svc.gotRepo != "r" || svc.gotNew != "dev" || svc.gotOld != "main" {
		t.Errorf("args not forwarded: %+v", svc)
	}
}

func TestCreateBranchConflictPropagates(t *testing.T) {
	want := domain.NewForgejoError(domain.KindConflict, "exists")
	svc := &stubBranchWriteService{err: want}
	if _, err := CreateBranch(context.Background(), svc, "o", "r", "main", "main"); !errors.Is(err, want) {
		t.Errorf("CreateBranch() error = %v, want %v", err, want)
	}
}

func TestCreateBranchValidation(t *testing.T) {
	svc := &stubBranchWriteService{}
	_, err := CreateBranch(context.Background(), svc, "o", "r", "", "main")
	if err == nil || !isValidation(err) {
		t.Errorf("expected validation error for empty branch name, got %v", err)
	}
}

// =============================================================================
// #18 forgejo_issue_create
// =============================================================================

func TestCreateIssue(t *testing.T) {
	svc := &stubIssueWriteService{issue: domain.Issue{Number: 7}}
	got, err := CreateIssue(context.Background(), svc, domain.CreateIssueInput{
		Owner: "o", Repo: "r", Title: "Bug", Body: "b", Labels: []int64{1}, Milestone: 3,
	})
	if err != nil {
		t.Fatalf("CreateIssue() error = %v", err)
	}
	if got.Number != 7 {
		t.Errorf("issue = %+v", got)
	}
	if svc.gotIn.Title != "Bug" || len(svc.gotIn.Labels) != 1 || svc.gotIn.Milestone != 3 {
		t.Errorf("input not forwarded: %+v", svc.gotIn)
	}
}

func TestCreateIssueValidation(t *testing.T) {
	svc := &stubIssueWriteService{}
	_, err := CreateIssue(context.Background(), svc, domain.CreateIssueInput{Owner: "o", Repo: "r", Title: ""})
	if err == nil || !isValidation(err) {
		t.Errorf("expected validation error for empty title, got %v", err)
	}
}

// =============================================================================
// #19 forgejo_issue_update — idempotent
// =============================================================================

func TestUpdateIssue(t *testing.T) {
	svc := &stubIssueWriteService{issue: domain.Issue{Number: 7, State: "closed"}}
	got, err := UpdateIssue(context.Background(), svc, domain.UpdateIssueInput{
		Owner: "o", Repo: "r", Index: 7, Title: "v2", Body: "new", State: "closed",
	})
	if err != nil {
		t.Fatalf("UpdateIssue() error = %v", err)
	}
	if got.State != "closed" {
		t.Errorf("issue = %+v", got)
	}
	if svc.gotUpd.Index != 7 || svc.gotUpd.State != "closed" {
		t.Errorf("input not forwarded: %+v", svc.gotUpd)
	}
}

func TestUpdateIssueIdempotent(t *testing.T) {
	svc := &stubIssueWriteService{issue: domain.Issue{Number: 7}}
	in := domain.UpdateIssueInput{Owner: "o", Repo: "r", Index: 7, Title: "v2", State: "closed"}
	for i := 0; i < 2; i++ {
		if _, err := UpdateIssue(context.Background(), svc, in); err != nil {
			t.Fatalf("UpdateIssue() iteration %d error = %v", i, err)
		}
	}
	if svc.updateCalls != 2 {
		t.Errorf("expected 2 idempotent calls, got %d", svc.updateCalls)
	}
}

// =============================================================================
// #20 forgejo_issue_comment_add — not idempotent
// =============================================================================

func TestAddIssueComment(t *testing.T) {
	svc := &stubIssueWriteService{comment: domain.Comment{ID: 9, Body: "me too"}}
	got, err := AddIssueComment(context.Background(), svc, "o", "r", 7, "me too")
	if err != nil {
		t.Fatalf("AddIssueComment() error = %v", err)
	}
	if got.ID != 9 {
		t.Errorf("comment = %+v", got)
	}
	if svc.gotIndex != 7 || svc.gotBody != "me too" {
		t.Errorf("args not forwarded: %+v", svc)
	}
}

func TestAddIssueCommentNotIdempotent(t *testing.T) {
	svc := &stubIssueWriteService{comment: domain.Comment{ID: 9}}
	for i := 0; i < 2; i++ {
		if _, err := AddIssueComment(context.Background(), svc, "o", "r", 7, "again"); err != nil {
			t.Fatalf("AddIssueComment() iteration %d error = %v", i, err)
		}
	}
	// Each call appends a new comment — the service is invoked every time.
	if svc.commentCalls != 2 {
		t.Errorf("expected 2 comment calls (new comment each), got %d", svc.commentCalls)
	}
}

func TestAddIssueCommentValidation(t *testing.T) {
	svc := &stubIssueWriteService{}
	_, err := AddIssueComment(context.Background(), svc, "o", "r", 7, "")
	if err == nil || !isValidation(err) {
		t.Errorf("expected validation error for empty body, got %v", err)
	}
}

// =============================================================================
// #21 forgejo_pull_create
// =============================================================================

func TestCreatePullRequest(t *testing.T) {
	svc := &stubPullWriteService{pr: domain.PullRequest{Number: 5}}
	got, err := CreatePullRequest(context.Background(), svc, domain.CreatePullRequestInput{
		Owner: "o", Repo: "r", Title: "PR", Body: "b", Head: "dev", Base: "main",
	})
	if err != nil {
		t.Fatalf("CreatePullRequest() error = %v", err)
	}
	if got.Number != 5 {
		t.Errorf("pr = %+v", got)
	}
	if svc.gotCreate.Head != "dev" || svc.gotCreate.Base != "main" || svc.gotCreate.Title != "PR" {
		t.Errorf("input not forwarded: %+v", svc.gotCreate)
	}
}

func TestCreatePullRequestValidation(t *testing.T) {
	svc := &stubPullWriteService{}
	_, err := CreatePullRequest(context.Background(), svc, domain.CreatePullRequestInput{Owner: "o", Repo: "r", Head: "dev", Base: "main"})
	if err == nil || !isValidation(err) {
		t.Errorf("expected validation error for empty title, got %v", err)
	}
}

// =============================================================================
// #22 forgejo_pull_update — idempotent
// =============================================================================

func TestUpdatePullRequest(t *testing.T) {
	svc := &stubPullWriteService{pr: domain.PullRequest{Number: 5, State: "closed"}}
	got, err := UpdatePullRequest(context.Background(), svc, domain.UpdatePullRequestInput{
		Owner: "o", Repo: "r", Index: 5, Title: "v2", State: "closed",
	})
	if err != nil {
		t.Fatalf("UpdatePullRequest() error = %v", err)
	}
	if got.State != "closed" {
		t.Errorf("pr = %+v", got)
	}
	if svc.gotUpd.Index != 5 || svc.gotUpd.State != "closed" {
		t.Errorf("input not forwarded: %+v", svc.gotUpd)
	}
}

func TestUpdatePullRequestIdempotent(t *testing.T) {
	svc := &stubPullWriteService{pr: domain.PullRequest{Number: 5}}
	in := domain.UpdatePullRequestInput{Owner: "o", Repo: "r", Index: 5, Title: "v2", State: "open"}
	for i := 0; i < 2; i++ {
		if _, err := UpdatePullRequest(context.Background(), svc, in); err != nil {
			t.Fatalf("UpdatePullRequest() iteration %d error = %v", i, err)
		}
	}
	if svc.updateCalls != 2 {
		t.Errorf("expected 2 idempotent calls, got %d", svc.updateCalls)
	}
}

// =============================================================================
// #23 forgejo_pull_merge — merged vs already-merged
// =============================================================================

func TestMergePullRequestAlreadyMerged(t *testing.T) {
	svc := &stubPullWriteService{merged: true}
	res, err := MergePullRequest(context.Background(), svc, "o", "r", 5, "merge")
	if err != nil {
		t.Fatalf("MergePullRequest() error = %v", err)
	}
	if !res.AlreadyMerged || res.Merged {
		t.Errorf("expected already_merged, got %+v", res)
	}
	if svc.mergeCalls != 0 {
		t.Errorf("must not call merge when already merged, mergeCalls=%d", svc.mergeCalls)
	}
}

func TestMergePullRequestMerges(t *testing.T) {
	svc := &stubPullWriteService{merged: false}
	res, err := MergePullRequest(context.Background(), svc, "o", "r", 5, "squash")
	if err != nil {
		t.Fatalf("MergePullRequest() error = %v", err)
	}
	if !res.Merged || res.AlreadyMerged {
		t.Errorf("expected merged, got %+v", res)
	}
	if svc.mergeCalls != 1 || svc.gotMethod != "squash" || svc.gotIndex != 5 {
		t.Errorf("merge not invoked correctly: %+v", svc)
	}
}

func TestMergePullRequestValidation(t *testing.T) {
	svc := &stubPullWriteService{}
	_, err := MergePullRequest(context.Background(), svc, "o", "r", 5, "teleport")
	if err == nil || !isValidation(err) {
		t.Errorf("expected validation error for invalid method, got %v", err)
	}
	if svc.mergeCalls != 0 {
		t.Errorf("service should not be called on validation failure")
	}
}

func TestMergePullRequestMergeError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindTransient, "boom")
	svc := &stubPullWriteService{mergeErr: want}
	_, err := MergePullRequest(context.Background(), svc, "o", "r", 5, "merge")
	if !errors.Is(err, want) {
		t.Errorf("MergePullRequest() error = %v, want %v", err, want)
	}
}

// =============================================================================
// #24 forgejo_pull_review — create AND submit in one call
// =============================================================================

func TestReviewPullRequestCreatesThenSubmits(t *testing.T) {
	svc := &stubPullWriteService{}
	review, err := ReviewPullRequest(context.Background(), svc, "o", "r", 5, "looks good", "APPROVED")
	if err != nil {
		t.Fatalf("ReviewPullRequest() error = %v", err)
	}
	if svc.reviewCreateCalls != 1 || svc.reviewSubmitCalls != 1 {
		t.Errorf("expected create+submit, create=%d submit=%d", svc.reviewCreateCalls, svc.reviewSubmitCalls)
	}
	// The submit must reference the review ID produced by create.
	if svc.gotReviewID != 11 {
		t.Errorf("submit reviewID = %d, want 11", svc.gotReviewID)
	}
	if svc.gotEvent != "APPROVED" {
		t.Errorf("event = %q, want APPROVED", svc.gotEvent)
	}
	if review.State != "APPROVED" {
		t.Errorf("review = %+v", review)
	}
}

func TestReviewPullRequestValidation(t *testing.T) {
	svc := &stubPullWriteService{}
	_, err := ReviewPullRequest(context.Background(), svc, "o", "r", 5, "b", "wat")
	if err == nil || !isValidation(err) {
		t.Errorf("expected validation error for invalid event, got %v", err)
	}
	if svc.reviewCreateCalls != 0 {
		t.Errorf("service should not be called on validation failure")
	}
}

// =============================================================================
// #25 forgejo_release_create
// =============================================================================

func TestCreateRelease(t *testing.T) {
	svc := &stubReleaseWriteService{release: domain.Release{TagName: "v1.0"}}
	got, err := CreateRelease(context.Background(), svc, domain.CreateReleaseInput{
		Owner: "o", Repo: "r", Tag: "v1.0", Name: "V1.0", Notes: "notes",
	})
	if err != nil {
		t.Fatalf("CreateRelease() error = %v", err)
	}
	if got.TagName != "v1.0" {
		t.Errorf("release = %+v", got)
	}
	if svc.gotIn.Tag != "v1.0" || svc.gotIn.Name != "V1.0" || svc.gotIn.Notes != "notes" {
		t.Errorf("input not forwarded: %+v", svc.gotIn)
	}
}

func TestCreateReleaseValidation(t *testing.T) {
	svc := &stubReleaseWriteService{}
	_, err := CreateRelease(context.Background(), svc, domain.CreateReleaseInput{Owner: "o", Repo: "r", Tag: ""})
	if err == nil || !isValidation(err) {
		t.Errorf("expected validation error for empty tag, got %v", err)
	}
}
