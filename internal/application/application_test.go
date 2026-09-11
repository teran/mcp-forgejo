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

// =============================================================================
// Batch A read tools (SPEC 6.1 #1, #6, #7, #8, #9, #11, #12, #13).
// Stub services implement the domain interfaces @developer must add.
// =============================================================================

type stubSearchService struct {
	repos                   []domain.Repository
	err                     error
	gotQ, gotTopic, gotSort string
	gotOrder                string
	gotPrivate              *bool
	searchCalls             int
}

func (s *stubSearchService) SearchRepos(_ context.Context, q, topic, sort, order string, private *bool) ([]domain.Repository, error) {
	s.searchCalls++
	s.gotQ, s.gotTopic, s.gotSort, s.gotOrder, s.gotPrivate = q, topic, sort, order, private
	return s.repos, s.err
}

type stubDiffService struct {
	diff      domain.Diff
	err       error
	baseCalls int
	pullCalls int
	gotOwner  string
	gotRepo   string
	gotBase   string
	gotIndex  int64
}

func (s *stubDiffService) GetDiff(_ context.Context, owner, repo, basehead string) (domain.Diff, error) {
	s.baseCalls++
	s.gotOwner, s.gotRepo, s.gotBase = owner, repo, basehead
	return s.diff, s.err
}

func (s *stubDiffService) GetPullDiff(_ context.Context, owner, repo string, index int64) (domain.Diff, error) {
	s.pullCalls++
	s.gotOwner, s.gotRepo, s.gotIndex = owner, repo, index
	return s.diff, s.err
}

type stubCommitService struct {
	commits []domain.Commit
	err     error
}

func (s *stubCommitService) ListCommits(_ context.Context, _, _, _ string, _, _ int) ([]domain.Commit, error) {
	return s.commits, s.err
}

type stubBranchService struct {
	branches []domain.Branch
	err      error
}

func (s *stubBranchService) ListBranches(_ context.Context, _, _ string) ([]domain.Branch, error) {
	return s.branches, s.err
}

type stubIssueListService struct {
	issues []domain.Issue
	err    error
}

func (s *stubIssueListService) ListIssues(_ context.Context, _, _, _ string, _, _ int) ([]domain.Issue, error) {
	return s.issues, s.err
}

type stubPullService struct {
	prs    []domain.PullRequest
	detail domain.PullRequestDetail
	err    error
}

func (s *stubPullService) ListPullRequests(_ context.Context, _, _, _ string, _, _ int) ([]domain.PullRequest, error) {
	return s.prs, s.err
}

func (s *stubPullService) GetPullRequest(_ context.Context, _, _ string, _ int64) (domain.PullRequestDetail, error) {
	return s.detail, s.err
}

type stubReleaseService struct {
	releases []domain.Release
	latest   domain.Release
	err      error
}

func (s *stubReleaseService) ListReleases(_ context.Context, _, _ string, _, _ int) ([]domain.Release, error) {
	return s.releases, s.err
}

func (s *stubReleaseService) GetLatestRelease(_ context.Context, _, _ string) (domain.Release, error) {
	return s.latest, s.err
}

func TestSearchRepos(t *testing.T) {
	priv := true
	svc := &stubSearchService{repos: []domain.Repository{{Name: "r"}}}
	got, err := SearchRepos(context.Background(), svc, "go", "topic", "stars", "asc", &priv)
	if err != nil {
		t.Fatalf("SearchRepos() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "r" {
		t.Errorf("got = %+v", got)
	}
	if svc.gotQ != "go" || svc.gotTopic != "topic" || svc.gotSort != "stars" || svc.gotOrder != "asc" || svc.gotPrivate == nil || !*svc.gotPrivate {
		t.Errorf("args not forwarded: %+v", svc)
	}
}

func TestSearchReposError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindTransient, "boom")
	svc := &stubSearchService{err: want}
	if _, err := SearchRepos(context.Background(), svc, "q", "", "", "", nil); !errors.Is(err, want) {
		t.Errorf("SearchRepos() error = %v, want %v", err, want)
	}
}

func TestGetDiffByBasehead(t *testing.T) {
	svc := &stubDiffService{diff: domain.Diff{BaseHead: "main..dev", Text: "x"}}
	got, err := GetDiff(context.Background(), svc, "o", "r", "main..dev", 0)
	if err != nil {
		t.Fatalf("GetDiff() error = %v", err)
	}
	if svc.baseCalls != 1 || svc.pullCalls != 0 {
		t.Errorf("expected basehead call, baseCalls=%d pullCalls=%d", svc.baseCalls, svc.pullCalls)
	}
	if svc.gotBase != "main..dev" || got.Text != "x" {
		t.Errorf("got = %+v, svc = %+v", got, svc)
	}
}

func TestGetDiffByPull(t *testing.T) {
	svc := &stubDiffService{diff: domain.Diff{Text: "pr diff"}}
	got, err := GetDiff(context.Background(), svc, "o", "r", "", 7)
	if err != nil {
		t.Fatalf("GetDiff() error = %v", err)
	}
	if svc.pullCalls != 1 || svc.baseCalls != 0 {
		t.Errorf("expected pull call, baseCalls=%d pullCalls=%d", svc.baseCalls, svc.pullCalls)
	}
	if svc.gotIndex != 7 || got.Text != "pr diff" {
		t.Errorf("got = %+v, svc = %+v", got, svc)
	}
}

func TestGetDiffError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindNotFound, "nope")
	svc := &stubDiffService{err: want}
	if _, err := GetDiff(context.Background(), svc, "o", "r", "main..dev", 0); !errors.Is(err, want) {
		t.Errorf("GetDiff() error = %v, want %v", err, want)
	}
}

func TestListCommits(t *testing.T) {
	svc := &stubCommitService{commits: []domain.Commit{{SHA: "abc", Message: "m"}}}
	got, err := ListCommits(context.Background(), svc, "o", "r", "main", 1, 20)
	if err != nil {
		t.Fatalf("ListCommits() error = %v", err)
	}
	if len(got) != 1 || got[0].SHA != "abc" {
		t.Errorf("got = %+v", got)
	}
}

func TestListCommitsError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindTransient, "boom")
	svc := &stubCommitService{err: want}
	if _, err := ListCommits(context.Background(), svc, "o", "r", "", 0, 0); !errors.Is(err, want) {
		t.Errorf("ListCommits() error = %v, want %v", err, want)
	}
}

func TestListBranches(t *testing.T) {
	svc := &stubBranchService{branches: []domain.Branch{{Name: "main"}}}
	got, err := ListBranches(context.Background(), svc, "o", "r")
	if err != nil {
		t.Fatalf("ListBranches() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "main" {
		t.Errorf("got = %+v", got)
	}
}

func TestListBranchesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindForbidden, "nope")
	svc := &stubBranchService{err: want}
	if _, err := ListBranches(context.Background(), svc, "o", "r"); !errors.Is(err, want) {
		t.Errorf("ListBranches() error = %v, want %v", err, want)
	}
}

func TestListIssues(t *testing.T) {
	svc := &stubIssueListService{issues: []domain.Issue{{Number: 1, State: "open"}}}
	got, err := ListIssues(context.Background(), svc, "o", "r", "open", 1, 10)
	if err != nil {
		t.Fatalf("ListIssues() error = %v", err)
	}
	if len(got) != 1 || got[0].Number != 1 || got[0].State != "open" {
		t.Errorf("got = %+v", got)
	}
}

func TestListIssuesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindNotFound, "nope")
	svc := &stubIssueListService{err: want}
	if _, err := ListIssues(context.Background(), svc, "o", "r", "all", 0, 0); !errors.Is(err, want) {
		t.Errorf("ListIssues() error = %v, want %v", err, want)
	}
}

func TestListPullRequests(t *testing.T) {
	svc := &stubPullService{prs: []domain.PullRequest{{Number: 1, State: "closed"}}}
	got, err := ListPullRequests(context.Background(), svc, "o", "r", "closed", 1, 30)
	if err != nil {
		t.Fatalf("ListPullRequests() error = %v", err)
	}
	if len(got) != 1 || got[0].Number != 1 || got[0].State != "closed" {
		t.Errorf("got = %+v", got)
	}
}

func TestListPullRequestsError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindTransient, "boom")
	svc := &stubPullService{err: want}
	if _, err := ListPullRequests(context.Background(), svc, "o", "r", "", 0, 0); !errors.Is(err, want) {
		t.Errorf("ListPullRequests() error = %v, want %v", err, want)
	}
}

func TestGetPullRequest(t *testing.T) {
	svc := &stubPullService{detail: domain.PullRequestDetail{
		PullRequest: domain.PullRequest{Number: 5},
		Files:       []domain.PullFile{{Filename: "a.go"}},
		Checks:      []domain.Check{{Context: "ci"}},
	}}
	got, err := GetPullRequest(context.Background(), svc, "o", "r", 5)
	if err != nil {
		t.Fatalf("GetPullRequest() error = %v", err)
	}
	if got.PullRequest.Number != 5 || len(got.Files) != 1 || len(got.Checks) != 1 {
		t.Errorf("got = %+v", got)
	}
}

func TestGetPullRequestError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindNotFound, "nope")
	svc := &stubPullService{err: want}
	if _, err := GetPullRequest(context.Background(), svc, "o", "r", 5); !errors.Is(err, want) {
		t.Errorf("GetPullRequest() error = %v, want %v", err, want)
	}
}

func TestListReleasesAll(t *testing.T) {
	svc := &stubReleaseService{releases: []domain.Release{{TagName: "v1"}}}
	got, err := ListReleases(context.Background(), svc, "o", "r", false, 1, 20)
	if err != nil {
		t.Fatalf("ListReleases() error = %v", err)
	}
	if len(got) != 1 || got[0].TagName != "v1" {
		t.Errorf("got = %+v", got)
	}
}

func TestListReleasesLatest(t *testing.T) {
	svc := &stubReleaseService{latest: domain.Release{TagName: "v2"}}
	got, err := ListReleases(context.Background(), svc, "o", "r", true, 0, 0)
	if err != nil {
		t.Fatalf("ListReleases() error = %v", err)
	}
	if len(got) != 1 || got[0].TagName != "v2" {
		t.Errorf("got = %+v", got)
	}
}

func TestListReleasesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindTransient, "boom")
	svc := &stubReleaseService{err: want}
	if _, err := ListReleases(context.Background(), svc, "o", "r", false, 0, 0); !errors.Is(err, want) {
		t.Errorf("ListReleases() error = %v, want %v", err, want)
	}
}
