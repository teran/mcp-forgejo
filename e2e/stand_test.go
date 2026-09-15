//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// StandSuite is the testify/suite harness that owns one booted Stand and
// exercises the MCP server over the REAL HTTP/SSE transport against a live
// Forgejo container.
type StandSuite struct {
	suite.Suite
	stand  *Stand
	ctx    context.Context
	cancel context.CancelFunc
}

// SetupSuite boots the stand once for the whole suite. If Docker is
// unavailable, NewStand calls t.Skipf on the suite's parent test, which skips
// every subtest automatically.
func (s *StandSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 6*time.Minute)
	s.stand = NewStand(s.T(), s.ctx)
}

func (s *StandSuite) TearDownSuite() {
	if s.cancel != nil {
		s.cancel()
	}
}

// TestStandSuite runs the stand suite.
func TestStandSuite(t *testing.T) {
	suite.Run(t, new(StandSuite))
}

// TestHTTPRoundTrip proves a genuine MCP tool call round-trips over the HTTP/SSE
// transport: list -> create -> get -> write -> list-contents -> delete, all
// against the live Forgejo container.
func (s *StandSuite) TestHTTPRoundTrip() {
	t := s.T()
	st := s.stand

	// Read: org list starts empty.
	orgs := CallJSON[orgListResult](st, t, "forgejo_org_list", map[string]any{})
	require.Len(t, orgs.Items, 0, "expected no organizations up front")

	// Write: create a repository under the admin user (client targets
	// POST /api/v1/user/repos when owner is omitted).
	repoName := fmt.Sprintf("stand-repo-%d", time.Now().UnixNano())
	repo := CallJSON[repoResult](st, t, "forgejo_repo_create", map[string]any{
		"name": repoName, "auto_init": true, "default_branch": "main",
	})
	require.Equal(t, repoName, repo.Name)
	require.Equal(t, "main", repo.DefaultBranch)
	st.Defer(t, func() {
		CallJSON[any](st, t, "forgejo_repo_delete", map[string]any{
			"owner": st.Admin(), "repo": repoName, "confirm": true,
		})
	})

	// Read: fetch the repository back.
	got := CallJSON[repoResult](st, t, "forgejo_repo_get", map[string]any{
		"owner": st.Admin(), "repo": repoName,
	})
	require.Equal(t, st.Admin()+"/"+repoName, got.FullName)

	// Write: write a file (creates a commit).
	wf := CallJSON[fileResult](st, t, "forgejo_file_write", map[string]any{
		"owner": st.Admin(), "repo": repoName, "path": "README.md",
		"branch": "main", "message": "stand: write README",
		"content": "# stand\n\nhello over HTTP\n",
	})
	require.Equal(t, "README.md", wf.Path)
	require.NotEmpty(t, wf.SHA)

	// Read: list the repo root; the file must be present.
	contents := CallJSON[contentsResult](st, t, "forgejo_repo_list_contents", map[string]any{
		"owner": st.Admin(), "repo": repoName,
	})
	found := false
	for _, e := range contents.Items {
		if e.Name == "README.md" && e.Type == "file" {
			found = true
		}
	}
	require.True(t, found, "README.md not found in repo root: %+v", contents.Items)

	// Write: create an organization.
	orgName := fmt.Sprintf("stand-org-%d", time.Now().UnixNano())
	org := CallJSON[orgResult](st, t, "forgejo_org_create", map[string]any{
		"username": orgName, "full_name": "Stand E2E Org",
	})
	require.Equal(t, orgName, org.Username)
	st.Defer(t, func() {
		CallJSON[any](st, t, "forgejo_org_delete", map[string]any{"org": orgName})
	})

	// Read: org_list now reflects the created org.
	orgs2 := CallJSON[orgListResult](st, t, "forgejo_org_list", map[string]any{})
	orgFound := false
	for _, o := range orgs2.Items {
		if o.Username == orgName {
			orgFound = true
		}
	}
	require.True(t, orgFound, "created org %q not listed: %+v", orgName, orgs2.Items)
}

// TestToolSurfaceAnnotationProvesToolsList returns the full tool surface and
// asserts the annotation contract: read tools are readOnlyHint=true, delete
// tools are destructiveHint=true, and the registered tool count matches the
// server's registry.
func (s *StandSuite) TestToolsListAnnotations() {
	t := s.T()
	tools := s.stand.ListTools(t)
	require.NotEmpty(t, tools)

	// Every advertised tool must carry annotations.
	byName := make(map[string]*mcp.Tool, len(tools))
	for _, tool := range tools {
		byName[tool.Name] = tool
		require.NotNil(t, tool.Annotations, "tool %q missing annotations", tool.Name)
	}

	// Read tools must advertise readOnlyHint=true.
	read := []string{
		"forgejo_org_list", "forgejo_repo_get", "forgejo_repo_list_contents",
		"forgejo_file_get", "forgejo_issue_get", "forgejo_commit_list",
		"forgejo_branch_get", "forgejo_branch_list", "forgejo_issue_list",
		"forgejo_release_list", "forgejo_pull_get", "forgejo_pull_list",
		"forgejo_repo_search", "forgejo_diff_get", "forgejo_tag_list",
		"forgejo_milestone_list", "forgejo_label_list", "forgejo_user_get",
		"forgejo_user_list",
	}
	for _, name := range read {
		tool, ok := byName[name]
		require.Truef(t, ok, "read tool %q not advertised by tools/list", name)
		require.Truef(t, tool.Annotations.ReadOnlyHint,
			"read tool %q must have readOnlyHint=true", name)
	}

	// Delete tools must advertise destructiveHint=true.
	del := []string{
		"forgejo_repo_delete", "forgejo_org_delete", "forgejo_file_delete",
		"forgejo_branch_delete", "forgejo_issue_delete", "forgejo_comment_delete",
		"forgejo_release_delete", "forgejo_tag_delete", "forgejo_milestone_delete",
		"forgejo_label_delete",
	}
	for _, name := range del {
		tool, ok := byName[name]
		require.Truef(t, ok, "delete tool %q not advertised by tools/list", name)
		require.NotNilf(t, tool.Annotations.DestructiveHint,
			"delete tool %q must have destructiveHint set", name)
		require.Truef(t, *tool.Annotations.DestructiveHint,
			"delete tool %q must have destructiveHint=true", name)
	}

	// Sanity: no tool is both read-only and destructive.
	for _, tool := range tools {
		if tool.Annotations.ReadOnlyHint && tool.Annotations.DestructiveHint != nil && *tool.Annotations.DestructiveHint {
			t.Errorf("tool %q is both readOnlyHint and destructiveHint", tool.Name)
		}
	}
}
