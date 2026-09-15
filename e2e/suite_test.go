//go:build e2e

// Package e2e hosts the comprehensive testify/suite end-to-end suite. It boots
// one REAL Forgejo container + in-process MCP server over the HTTP/SSE
// transport (via the Stand) and drives every registered tool through the MCP
// protocol, asserting observable results, the SPEC §4.4 error taxonomy and the
// S2 data-hygiene guarantees (the PAT never surfaces in any tool output or
// error text).
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/teran/mcp-forgejo/internal/infrastructure/forgejo"
	"github.com/teran/mcp-forgejo/internal/server"
)

// Result shapes mirroring the JSON emitted by the MCP server. Only the fields
// asserted on are declared.
type (
	issueResult struct {
		ID       int64         `json:"id"`
		Number   int64         `json:"number"`
		Title    string        `json:"title"`
		Body     string        `json:"body"`
		State    string        `json:"state"`
		Comments int64         `json:"comments"`
		Labels   []labelResult `json:"labels"`
	}

	issueWithCommentsResult struct {
		Issue    issueResult     `json:"issue"`
		Comments []commentResult `json:"comments"`
	}

	commentResult struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
	}

	pullResult struct {
		ID     int64  `json:"id"`
		Number int64  `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		State  string `json:"state"`
	}

	pullDetailResult struct {
		PullRequest pullResult `json:"pull_request"`
		Files       []pullFile `json:"files"`
		Checks      []struct{} `json:"checks"`
	}

	pullFile struct {
		Filename string `json:"filename"`
		Status   string `json:"status"`
	}

	releaseResult struct {
		ID      int64  `json:"id"`
		TagName string `json:"tag_name"`
		Name    string `json:"name"`
		Body    string `json:"body"`
	}

	releaseListResult struct {
		Items []releaseResult `json:"items"`
	}

	releaseAssetResult struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		Size int64  `json:"size"`
	}

	tagResult struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		SHA  string `json:"sha"`
	}

	tagListResult struct {
		Items []tagResult `json:"items"`
	}

	milestoneResult struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
		State string `json:"state"`
	}

	milestoneListResult struct {
		Items []milestoneResult `json:"items"`
	}

	labelResult struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Color string `json:"color"`
	}

	labelListResult struct {
		Items []labelResult `json:"items"`
	}

	branchResult struct {
		Name      string `json:"name"`
		Protected bool   `json:"protected"`
		Default   bool   `json:"default"`
		CommitSHA string `json:"commit_sha"`
	}

	branchListResult struct {
		Items []branchResult `json:"items"`
	}

	commitResult struct {
		SHA     string `json:"sha"`
		Message string `json:"message"`
	}

	commitListResult struct {
		Items []commitResult `json:"items"`
	}

	userResult struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}

	userListResult struct {
		Items []userResult `json:"items"`
	}

	diffResult struct {
		BaseHead string `json:"basehead"`
		Text     string `json:"text"`
	}

	changeFilesResult struct {
		CommitSHA string             `json:"commit_sha"`
		Files     []changeFileResult `json:"files"`
	}

	changeFileResult struct {
		Path   string `json:"path"`
		SHA    string `json:"sha"`
		Status string `json:"status"`
	}

	reviewResult struct {
		ID    int64  `json:"id"`
		Body  string `json:"body"`
		State string `json:"state"`
	}

	pullMergeResult struct {
		Merged        bool `json:"merged"`
		AlreadyMerged bool `json:"already_merged"`
	}
)

// FullSuite is the comprehensive e2e suite. SetupSuite boots ONE Stand and
// builds a full fixture graph (org, repo, dev branch, tag, release, labels,
// milestone, issue + comment, PR, fork) via the MCP tools themselves, capturing
// ids/shas/numbers into the suite for the read/write/delete tests to reuse.
type FullSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	stand  *Stand

	// namespace isolates every name created by the suite.
	ns string

	// fixtures (created in SetupSuite via MCP tools).
	orgName       string
	repoName      string
	devBranch     string
	tagName       string
	labelID       int64
	milestoneID   int64
	issueIndex    int64
	issueID       int64
	commentID     int64
	releaseID     int64
	prNumber      int64
	prID          int64
	forkName      string
	mainCommitSHA string

	// invoked tracks tool name -> test method that invoked it (coverage proof).
	invoked map[string]string
}

// TestFullSuite runs the comprehensive suite.
func TestFullSuite(t *testing.T) {
	suite.Run(t, new(FullSuite))
}

// SetupSuite boots the stand and builds the shared fixture graph. If Docker is
// unavailable NewStand skips the suite (every subtest auto-skips).
func (s *FullSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 15*time.Minute)
	s.stand = NewStand(s.T(), s.ctx)
	s.invoked = map[string]string{}
	s.ns = fmt.Sprintf("e2e-%d", time.Now().UnixNano())

	s.buildFixtures()
}

func (s *FullSuite) TearDownSuite() {
	// NOTE: we deliberately do NOT cancel s.ctx here. Fixture teardown (org /
	// repo deletes) is registered via s.stand.Defer on the suite's parent test
	// and runs as a parent t.Cleanup AFTER TearDownSuite returns; those cleanup
	// calls still need a live context. The context's own 15m timeout bounds its
	// lifetime, and stand.Close tears down the client/server afterwards.
}

// buildFixtures dogfoods the MCP write tools to assemble the shared fixture
// graph used by the read/write/delete tests. Teardown (reverse dependency
// order, tolerating already-gone objects) is registered on the suite's parent
// test so it runs after every subtest but before Close.
func (s *FullSuite) buildFixtures() {
	t := s.T()

	// Organization.
	orgName := s.ns + "-org"
	org := callSuiteJSON[orgResult](s, t, "forgejo_org_create", map[string]any{
		"username": orgName, "full_name": "E2E Org " + s.ns,
	})
	require.Equal(t, orgName, org.Username)
	s.orgName = orgName
	s.stand.Defer(t, func() {
		CallJSON[any](s.stand, t, "forgejo_org_delete", map[string]any{"org": s.orgName})
	})

	// Fixture repo under the admin user (owner omitted -> current user).
	repoName := s.ns + "-repo"
	repo := callSuiteJSON[repoResult](s, t, "forgejo_repo_create", map[string]any{
		"name": repoName, "auto_init": true, "default_branch": "main",
	})
	require.Equal(t, repoName, repo.Name)
	require.Equal(t, "main", repo.DefaultBranch)
	s.repoName = repoName

	// Write a file on main (updates the auto-generated README).
	wf := callSuiteJSON[fileResult](s, t, "forgejo_file_write", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "path": "README.md",
		"branch": "main", "message": "e2e: seed README", "content": "# " + s.repoName + "\n",
	})
	require.Equal(t, "README.md", wf.Path)
	require.NotEmpty(t, wf.SHA)
	s.mainCommitSHA = wf.CommitSHA

	// Create the dev branch and write a divergent file onto it.
	dev := s.ns + "-dev"
	devBranch := callSuiteJSON[branchResult](s, t, "forgejo_branch_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "new_branch": dev, "old_ref": "main",
	})
	require.Equal(t, dev, devBranch.Name)
	s.devBranch = dev
	callSuiteJSON[fileResult](s, t, "forgejo_file_write", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "path": "devfile.md",
		"branch": dev, "message": "e2e: seed devfile", "content": "# dev\n",
	})

	// Tag v1.0.0 at main.
	tagName := "v1.0.0"
	tag := callSuiteJSON[tagResult](s, t, "forgejo_tag_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "name": tagName, "target": "main",
	})
	require.Equal(t, tagName, tag.Name)
	s.tagName = tagName

	// Label.
	label := callSuiteJSON[labelResult](s, t, "forgejo_label_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "name": "bug",
		"color": "d73a4a", "description": "fixture label",
	})
	require.Equal(t, "bug", label.Name)
	require.NotZero(t, label.ID)
	s.labelID = label.ID

	// Milestone.
	ms := callSuiteJSON[milestoneResult](s, t, "forgejo_milestone_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "title": "Milestone One",
		"description": "fixture milestone", "due_on": "2027-12-31T00:00:00Z",
	})
	require.Equal(t, "Milestone One", ms.Title)
	require.NotZero(t, ms.ID)
	s.milestoneID = ms.ID

	// Issue (with label + milestone).
	iss := callSuiteJSON[issueResult](s, t, "forgejo_issue_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "title": "Fixture issue",
		"body": "from e2e", "labels": []int64{s.labelID}, "milestone": s.milestoneID,
	})
	require.Equal(t, "Fixture issue", iss.Title)
	require.NotZero(t, iss.Number)
	s.issueIndex = iss.Number
	s.issueID = iss.ID

	// Comment on the issue.
	cm := callSuiteJSON[commentResult](s, t, "forgejo_issue_comment_add", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": s.issueIndex, "body": "first comment",
	})
	require.Equal(t, "first comment", cm.Body)
	require.NotZero(t, cm.ID)
	s.commentID = cm.ID

	// Release for the existing tag.
	rel := callSuiteJSON[releaseResult](s, t, "forgejo_release_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "tag": s.tagName,
		"name": "Release " + s.tagName, "notes": "fixture release",
	})
	require.Equal(t, s.tagName, rel.TagName)
	require.NotZero(t, rel.ID)
	s.releaseID = rel.ID

	// Open PR dev -> main.
	pr := callSuiteJSON[pullResult](s, t, "forgejo_pull_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "head": s.devBranch,
		"base": "main", "title": "Fixture PR", "body": "from e2e",
	})
	require.Equal(t, "Fixture PR", pr.Title)
	require.NotZero(t, pr.Number)
	s.prNumber = pr.Number
	s.prID = pr.ID

	// Fork the fixture repo under a distinct name.
	forkName := s.ns + "-fork"
	fork := callSuiteJSON[repoResult](s, t, "forgejo_repo_fork", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "name": forkName,
	})
	require.Equal(t, forkName, fork.Name)
	s.forkName = forkName
}

// register records a tool invocation (coverage tracking). It resolves the
// calling test method from the stack.
func (s *FullSuite) register(tool string) {
	if s.invoked == nil {
		s.invoked = map[string]string{}
	}
	s.invoked[tool] = methodName(2)
}

// methodName returns the name of the function `skip` frames up the stack.
func methodName(skip int) string {
	pc, _, _, ok := runtime.Caller(skip)
	if !ok {
		return "unknown"
	}
	n := runtime.FuncForPC(pc).Name()
	if i := strings.LastIndex(n, "/"); i >= 0 {
		n = n[i+1:]
	}
	if i := strings.LastIndex(n, "."); i >= 0 {
		n = n[i+1:]
	}
	return n
}

// assertHygiene (S2) asserts the PAT never appears in a tool result's text.
func (s *FullSuite) assertHygiene(res *mcp.CallToolResult) {
	if res == nil {
		return
	}
	s.Assert().NotContainsf(textOf(res), s.stand.pat,
		"PAT leaked into tool output: %q", truncate(textOf(res)))
}

// callOK invokes a tool, fails on protocol/tool error, records coverage and
// checks data hygiene.
func (s *FullSuite) callOK(t *testing.T, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	s.register(tool)
	res, err := s.stand.Call(t, tool, args)
	require.NoErrorf(t, err, "CallTool(%s) error", tool)
	require.Falsef(t, res.IsError, "CallTool(%s) returned error: %s", tool, textOf(res))
	s.assertHygiene(res)
	return res
}

// callErr invokes a tool expecting a tool-level error (IsError), fails on
// protocol error, records coverage and checks data hygiene.
func (s *FullSuite) callErr(t *testing.T, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	s.register(tool)
	res, err := s.stand.Call(t, tool, args)
	require.NoErrorf(t, err, "CallTool(%s) transport error (expected tool-level error)", tool)
	require.Truef(t, res.IsError, "CallTool(%s) expected IsError, got success: %s", tool, textOf(res))
	msg := textOf(res)
	require.NotEmptyf(t, msg, "CallTool(%s) error text empty", tool)
	s.assertHygiene(res)
	return res
}

// callJSON invokes a tool, fails on error, records coverage, checks hygiene and
// decodes the JSON text content into T. It is a package-level generic function
// (methods cannot declare type parameters).
func callSuiteJSON[T any](s *FullSuite, t *testing.T, tool string, args map[string]any) T {
	t.Helper()
	var zero T
	res := s.callOK(t, tool, args)
	if len(res.Content) == 0 {
		return zero
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	require.Truef(t, ok, "CallTool(%s): expected TextContent, got %T", tool, res.Content[0])
	require.NoErrorf(t, json.Unmarshal([]byte(tc.Text), &zero),
		"CallTool(%s): decode result (text=%s)", tool, truncate(tc.Text))
	return zero
}

// rawRequest performs an authenticated Forgejo REST call for test setup
// (minting scoped tokens, archiving repos). auth is "basic" (admin credentials)
// or "token" (the raw PAT). The secret is only ever placed in the header.
func (s *FullSuite) rawRequest(t *testing.T, method, path string, body any, auth string) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(s.ctx, method, s.stand.BaseURL()+path, rdr)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	switch auth {
	case "basic":
		req.SetBasicAuth(s.stand.Admin(), s.stand.app.AdminPassword())
	default: // token
		req.Header.Set("Authorization", "token "+s.stand.pat)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// mintToken creates a PAT with the given scopes via raw REST (admin basic auth)
// and returns the token value. Test-only; never logged.
func (s *FullSuite) mintToken(t *testing.T, name string, scopes []string) string {
	t.Helper()
	code, raw := s.rawRequest(t, http.MethodPost,
		"/api/v1/users/"+s.stand.Admin()+"/tokens",
		map[string]any{"name": name, "scopes": scopes}, "basic")
	require.Equalf(t, http.StatusCreated, code, "mint token: %d %s", code, truncate(string(raw)))
	var tok struct {
		SHA1  string `json:"sha1"`
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(raw, &tok))
	if tok.SHA1 != "" {
		return tok.SHA1
	}
	require.NotEmpty(t, tok.Token)
	return tok.Token
}

// setArchived (un)archives a throwaway repo via raw REST using the raw PAT.
func (s *FullSuite) setArchived(t *testing.T, repo string, archived bool) {
	t.Helper()
	code, raw := s.rawRequest(t, http.MethodPatch,
		"/api/v1/repos/"+s.stand.Admin()+"/"+repo,
		map[string]any{"archived": archived}, "token")
	require.Containsf(t, []int{http.StatusOK, http.StatusNoContent}, code,
		"setArchived: %d %s", code, truncate(string(raw)))
}

// secondConn is a second in-process MCP client/server bound to the SAME Forgejo
// instance but with a different PAT (bogus or scoped), used for 401/403 tests.
type secondConn struct {
	cs    *mcp.ClientSession
	ts    *httptest.Server
	token string
}

// newSecondClient builds and connects a second in-process HTTP/SSE server+client
// against the same Forgejo with the given token.
func (s *FullSuite) newSecondClient(t *testing.T, token string) *secondConn {
	t.Helper()
	srv, err := server.Build(forgejo.Config{BaseURL: s.stand.BaseURL(), Token: token})
	require.NoError(t, err)
	ts := httptest.NewServer(server.NewHTTPHandler(srv))
	client := mcp.NewClient(&mcp.Implementation{Name: "e2e-second"}, nil)
	cs, err := client.Connect(s.ctx, &mcp.StreamableClientTransport{Endpoint: ts.URL}, nil)
	require.NoError(t, err)
	return &secondConn{cs: cs, ts: ts, token: token}
}

func (c *secondConn) call(t *testing.T, tool string, args map[string]any) (*mcp.CallToolResult, error) {
	t.Helper()
	return c.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
}

func (c *secondConn) close() {
	if c.cs != nil {
		_ = c.cs.Close()
	}
	if c.ts != nil {
		c.ts.Close()
	}
}

// The canonical inventory of the 51 registered tools (mirrors server.go).
var allTools = []string{
	// 19 READ
	"forgejo_repo_get", "forgejo_repo_list_contents", "forgejo_file_get",
	"forgejo_org_list", "forgejo_issue_get", "forgejo_repo_search",
	"forgejo_diff_get", "forgejo_commit_list", "forgejo_branch_list",
	"forgejo_issue_list", "forgejo_pull_list", "forgejo_pull_get",
	"forgejo_release_list", "forgejo_tag_list", "forgejo_milestone_list",
	"forgejo_label_list", "forgejo_user_get", "forgejo_user_list", "forgejo_branch_get",
	// 22 WRITE
	"forgejo_repo_create", "forgejo_org_create", "forgejo_file_write",
	"forgejo_file_write_many", "forgejo_branch_create", "forgejo_issue_create",
	"forgejo_issue_update", "forgejo_issue_comment_add", "forgejo_pull_create",
	"forgejo_pull_update", "forgejo_pull_merge", "forgejo_pull_review",
	"forgejo_release_create", "forgejo_tag_create", "forgejo_repo_fork",
	"forgejo_repo_update", "forgejo_milestone_create", "forgejo_milestone_update",
	"forgejo_label_create", "forgejo_label_update", "forgejo_issue_set_labels",
	"forgejo_release_asset_upload",
	// 10 DELETE
	"forgejo_file_delete", "forgejo_branch_delete", "forgejo_issue_delete",
	"forgejo_comment_delete", "forgejo_release_delete", "forgejo_repo_delete",
	"forgejo_org_delete", "forgejo_tag_delete", "forgejo_milestone_delete",
	"forgejo_label_delete",
}

// TestZCoverageAll51Tools (sorts last among Test* methods) proves every one of
// the 51 registered tools was explicitly invoked through the MCP protocol at
// least once, and that the advertised tool surface matches the canonical
// inventory.
func (s *FullSuite) TestZCoverageAll51Tools() {
	t := s.T()

	advertised := s.stand.ListTools(t)
	adByName := make(map[string]*mcp.Tool, len(advertised))
	for _, tool := range advertised {
		adByName[tool.Name] = tool
	}
	require.Equal(t, len(allTools), len(advertised),
		"advertised tool count does not match canonical inventory")

	for _, tool := range allTools {
		s.Assert().Containsf(adByName, tool, "tool %q not advertised", tool)
		s.Assert().Containsf(s.invoked, tool,
			"tool %q was never invoked through the MCP protocol", tool)
	}
}
