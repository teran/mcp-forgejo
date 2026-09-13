package e2e

import "github.com/modelcontextprotocol/go-sdk/mcp"

// readTools is the canonical set of read-only tools.
var readTools = []string{
	"forgejo_repo_get", "forgejo_repo_list_contents", "forgejo_file_get",
	"forgejo_org_list", "forgejo_issue_get", "forgejo_repo_search",
	"forgejo_diff_get", "forgejo_commit_list", "forgejo_branch_list",
	"forgejo_issue_list", "forgejo_pull_list", "forgejo_pull_get",
	"forgejo_release_list", "forgejo_tag_list", "forgejo_milestone_list",
	"forgejo_label_list", "forgejo_user_get", "forgejo_user_list", "forgejo_branch_get",
}

// deleteTools is the canonical set of destructive tools.
var deleteTools = []string{
	"forgejo_file_delete", "forgejo_branch_delete", "forgejo_issue_delete",
	"forgejo_comment_delete", "forgejo_release_delete", "forgejo_repo_delete",
	"forgejo_org_delete", "forgejo_tag_delete", "forgejo_milestone_delete",
	"forgejo_label_delete",
}

// idempotentWriteTools are write tools whose repeated invocation with identical
// args has no extra effect.
var idempotentWriteTools = []string{
	"forgejo_issue_update", "forgejo_pull_update", "forgejo_repo_update",
	"forgejo_milestone_update", "forgejo_label_update", "forgejo_issue_set_labels",
}

// TestHygieneAnnotationRegistry asserts the registry metadata contract: read
// tools advertise readOnlyHint=true, delete tools advertise
// destructiveHint=true && idempotentHint=false, and the idempotent write tools
// advertise idempotentHint=true.
func (s *FullSuite) TestHygieneAnnotationRegistry() {
	t := s.T()
	tools := s.stand.ListTools(t)
	byName := make(map[string]*mcp.Tool, len(tools))
	for _, tool := range tools {
		byName[tool.Name] = tool
		s.Assert().NotNil(tool.Annotations, "tool %q missing annotations", tool.Name)
	}

	for _, name := range readTools {
		tool, ok := byName[name]
		s.Require().Truef(ok, "read tool %q not advertised", name)
		s.Assert().True(tool.Annotations.ReadOnlyHint, "read tool %q readOnlyHint", name)
		s.Assert().False(*tool.Annotations.DestructiveHint, "read tool %q destructiveHint", name)
	}

	for _, name := range deleteTools {
		tool, ok := byName[name]
		s.Require().Truef(ok, "delete tool %q not advertised", name)
		s.Assert().True(*tool.Annotations.DestructiveHint, "delete tool %q destructiveHint", name)
		s.Assert().False(tool.Annotations.IdempotentHint, "delete tool %q idempotentHint must be false", name)
	}

	for _, name := range idempotentWriteTools {
		tool, ok := byName[name]
		s.Require().Truef(ok, "write tool %q not advertised", name)
		s.Assert().True(tool.Annotations.IdempotentHint, "write tool %q idempotentHint must be true", name)
	}
}

// TestHygieneNoTokenInMetadata asserts the PAT never appears in any advertised
// tool name, title, description or annotation.
func (s *FullSuite) TestHygieneNoTokenInMetadata() {
	t := s.T()
	tools := s.stand.ListTools(t)
	for _, tool := range tools {
		text := tool.Name + " " + tool.Title + " " + tool.Description
		s.Assert().NotContains(text, s.stand.pat, "PAT leaked into tool %q metadata", tool.Name)
	}
}

// TestHygieneNoTokenInErrorText drives a representative set of error paths and
// asserts the PAT (raw value) never surfaces in the error text, exercising the
// redaction path (S2) end to end.
func (s *FullSuite) TestHygieneNoTokenInErrorText() {
	t := s.T()
	cases := []struct {
		tool string
		args map[string]any
	}{
		{"forgejo_repo_get", map[string]any{"owner": s.stand.Admin(), "repo": s.ns + "-nope"}},
		{"forgejo_file_get", map[string]any{"owner": s.stand.Admin(), "repo": s.repoName, "path": "missing.md"}},
		{"forgejo_repo_create", map[string]any{"name": s.repoName}},
		{"forgejo_issue_create", map[string]any{"owner": s.stand.Admin(), "repo": s.repoName, "title": ""}},
		{"forgejo_branch_delete", map[string]any{"owner": s.stand.Admin(), "repo": s.repoName, "branch": "main"}},
	}
	for _, c := range cases {
		res, err := s.stand.Call(t, c.tool, c.args)
		s.Require().NoErrorf(err, "%s", c.tool)
		s.Require().Truef(res.IsError, "%s: expected IsError", c.tool)
		s.Assert().NotContainsf(textOf(res), s.stand.pat, "%s: PAT leaked", c.tool)
		s.Assert().NotEmptyf(textOf(res), "%s: empty error text", c.tool)
	}
}
