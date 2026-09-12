package application

// Batch U use cases (SPEC §6): forgejo_user_get, forgejo_user_list,
// forgejo_release_asset_upload, forgejo_branch_get. Test expectations for
// @developer: the application functions (orchestration + validation) and the
// domain interfaces they must call. The stub services below implement the
// domain interfaces @developer must add to internal/domain.

import (
	"context"
	"errors"
	"testing"

	"example.com/teran/mcp-forgejo/internal/domain"
)

// --- stubs for the new domain interfaces ------------------------------------

type stubUserService struct {
	user        domain.User
	err         error
	calls       int
	gotUsername string
}

func (s *stubUserService) GetUser(_ context.Context, username string) (domain.User, error) {
	s.calls++
	s.gotUsername = username
	return s.user, s.err
}

type stubUserSearchService struct {
	users []domain.User
	err   error
	calls int
	gotQ  string
}

func (s *stubUserSearchService) SearchUsers(_ context.Context, q string) ([]domain.User, error) {
	s.calls++
	s.gotQ = q
	return s.users, s.err
}

type stubReleaseAssetService struct {
	asset             domain.ReleaseAsset
	err               error
	calls             int
	gotOwner, gotRepo string
	gotReleaseID      int64
	gotFilename       string
	gotContent        []byte
}

func (s *stubReleaseAssetService) UploadReleaseAsset(_ context.Context, owner, repo string, releaseID int64, filename string, content []byte) (domain.ReleaseAsset, error) {
	s.calls++
	s.gotOwner, s.gotRepo = owner, repo
	s.gotReleaseID = releaseID
	s.gotFilename = filename
	s.gotContent = content
	return s.asset, s.err
}

type stubBranchReadService struct {
	branch            domain.Branch
	err               error
	calls             int
	gotOwner, gotRepo string
	gotBranch         string
}

func (s *stubBranchReadService) GetBranch(_ context.Context, owner, repo, branch string) (domain.Branch, error) {
	s.calls++
	s.gotOwner, s.gotRepo = owner, repo
	s.gotBranch = branch
	return s.branch, s.err
}

// =============================================================================
// forgejo_user_get — passthrough (empty username = current user)
// =============================================================================

func TestGetUser(t *testing.T) {
	svc := &stubUserService{user: domain.User{ID: 5, Login: "alice"}}
	got, err := GetUser(context.Background(), svc, "alice")
	if err != nil {
		t.Fatalf("GetUser() error = %v", err)
	}
	if got.Login != "alice" || got.ID != 5 {
		t.Errorf("user = %+v", got)
	}
	if svc.calls != 1 || svc.gotUsername != "alice" {
		t.Errorf("args not forwarded: %+v", svc)
	}
}

func TestGetUserEmptyUsernamePassthrough(t *testing.T) {
	// An empty username means "current user"; it must still be passed through
	// to the service (which routes it to /api/v1/user).
	svc := &stubUserService{user: domain.User{ID: 5, Login: "alice"}}
	if _, err := GetUser(context.Background(), svc, ""); err != nil {
		t.Fatalf("GetUser() error = %v", err)
	}
	if svc.calls != 1 || svc.gotUsername != "" {
		t.Errorf("empty username not forwarded: %+v", svc)
	}
}

func TestGetUserPropagatesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindNotFound, "missing")
	svc := &stubUserService{err: want}
	if _, err := GetUser(context.Background(), svc, "alice"); !errors.Is(err, want) {
		t.Errorf("GetUser() error = %v, want %v", err, want)
	}
}

// =============================================================================
// forgejo_user_list — passthrough
// =============================================================================

func TestSearchUsers(t *testing.T) {
	svc := &stubUserSearchService{users: []domain.User{{Login: "alice"}, {Login: "bob"}}}
	got, err := SearchUsers(context.Background(), svc, "alice")
	if err != nil {
		t.Fatalf("SearchUsers() error = %v", err)
	}
	if len(got) != 2 || got[0].Login != "alice" || got[1].Login != "bob" {
		t.Errorf("users = %+v", got)
	}
	if svc.calls != 1 || svc.gotQ != "alice" {
		t.Errorf("args not forwarded: %+v", svc)
	}
}

func TestSearchUsersPropagatesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindTransient, "boom")
	svc := &stubUserSearchService{err: want}
	if _, err := SearchUsers(context.Background(), svc, "alice"); !errors.Is(err, want) {
		t.Errorf("SearchUsers() error = %v, want %v", err, want)
	}
}

// =============================================================================
// forgejo_release_asset_upload — validation (releaseID>0, filename, content)
// =============================================================================

func TestUploadReleaseAsset(t *testing.T) {
	svc := &stubReleaseAssetService{asset: domain.ReleaseAsset{ID: 99, Name: "demo.bin"}}
	content := []byte("payload")
	got, err := UploadReleaseAsset(context.Background(), svc, "acme", "demo", 5, "demo.bin", content)
	if err != nil {
		t.Fatalf("UploadReleaseAsset() error = %v", err)
	}
	if got.ID != 99 || got.Name != "demo.bin" {
		t.Errorf("asset = %+v", got)
	}
	if svc.calls != 1 || svc.gotOwner != "acme" || svc.gotRepo != "demo" || svc.gotReleaseID != 5 ||
		svc.gotFilename != "demo.bin" || string(svc.gotContent) != "payload" {
		t.Errorf("args not forwarded: %+v", svc)
	}
}

func TestUploadReleaseAssetPropagatesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindForbidden, "no permission")
	svc := &stubReleaseAssetService{err: want}
	if _, err := UploadReleaseAsset(context.Background(), svc, "acme", "demo", 5, "f.bin", []byte("x")); !errors.Is(err, want) {
		t.Errorf("UploadReleaseAsset() error = %v, want %v", err, want)
	}
}

func TestUploadReleaseAssetValidation(t *testing.T) {
	svc := &stubReleaseAssetService{}
	cases := []struct {
		name      string
		releaseID int64
		filename  string
		content   []byte
	}{
		{"non-positive release id", 0, "f.bin", []byte("x")},
		{"empty filename", 5, "", []byte("x")},
		{"empty content", 5, "f.bin", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := UploadReleaseAsset(context.Background(), svc, "acme", "demo", tc.releaseID, tc.filename, tc.content)
			if err == nil || !isValidation(err) {
				t.Errorf("expected validation error, got %v", err)
			}
		})
	}
	if svc.calls != 0 {
		t.Errorf("service should not be called on validation failure")
	}
}

// =============================================================================
// forgejo_branch_get — validation (branch required)
// =============================================================================

func TestGetBranch(t *testing.T) {
	svc := &stubBranchReadService{branch: domain.Branch{Name: "main", CommitSHA: "abc123"}}
	got, err := GetBranch(context.Background(), svc, "acme", "demo", "main")
	if err != nil {
		t.Fatalf("GetBranch() error = %v", err)
	}
	if got.Name != "main" || got.CommitSHA != "abc123" {
		t.Errorf("branch = %+v", got)
	}
	if svc.calls != 1 || svc.gotOwner != "acme" || svc.gotRepo != "demo" || svc.gotBranch != "main" {
		t.Errorf("args not forwarded: %+v", svc)
	}
}

func TestGetBranchPropagatesError(t *testing.T) {
	want := domain.NewForgejoError(domain.KindNotFound, "missing")
	svc := &stubBranchReadService{err: want}
	if _, err := GetBranch(context.Background(), svc, "acme", "demo", "main"); !errors.Is(err, want) {
		t.Errorf("GetBranch() error = %v, want %v", err, want)
	}
}

func TestGetBranchValidation(t *testing.T) {
	svc := &stubBranchReadService{}
	if _, err := GetBranch(context.Background(), svc, "acme", "demo", ""); err == nil || !isValidation(err) {
		t.Errorf("expected validation error for empty branch, got %v", err)
	}
	if svc.calls != 0 {
		t.Errorf("service should not be called on validation failure")
	}
}
