package server

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// callToolRaw calls a tool and returns the raw CallToolResult without decoding
// any content, so tests can inspect StructuredContent directly.
func callToolRaw(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s) error = %v", name, err)
	}
	if res.IsError {
		t.Fatalf("CallTool(%s) returned error: %v", name, textOf(t, res))
	}
	return res
}

// listStructuredItems extracts the "items" array from a list tool's
// StructuredContent, failing the test if the result is not a JSON object with
// an "items" array (the record contract that MCP clients require).
func listStructuredItems(t *testing.T, res *mcp.CallToolResult) []any {
	t.Helper()
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent is %T, want map[string]any (object), value=%v", res.StructuredContent, res.StructuredContent)
	}
	items, ok := sc["items"].([]any)
	if !ok {
		t.Fatalf("StructuredContent['items'] is %T, want []any (array), value=%v", sc["items"], sc["items"])
	}
	return items
}

// TestListToolsReturnObjectContract verifies that every list (RO) tool returns
// a JSON *object* ({ "items": [...] }) in StructuredContent rather than a bare
// array, which is what MCP clients expect (regression for "expected record,
// received array").
func TestListToolsReturnObjectContract(t *testing.T) {
	cs, _ := setup(t)

	tests := []struct {
		name string
		args map[string]any
		want int
	}{
		{name: "forgejo_org_list", args: map[string]any{}, want: 1},
		{name: "forgejo_repo_search", args: map[string]any{"q": "go", "private": true}, want: 1},
		{name: "forgejo_repo_list_contents", args: map[string]any{"owner": "acme", "repo": "demo", "path": ""}, want: 2},
		{name: "forgejo_commit_list", args: map[string]any{"owner": "acme", "repo": "demo", "branch": "main"}, want: 1},
		{name: "forgejo_branch_list", args: map[string]any{"owner": "acme", "repo": "demo"}, want: 1},
		{name: "forgejo_issue_list", args: map[string]any{"owner": "acme", "repo": "demo", "state": "open"}, want: 1},
		{name: "forgejo_pull_list", args: map[string]any{"owner": "acme", "repo": "demo", "state": "open"}, want: 1},
		{name: "forgejo_release_list", args: map[string]any{"owner": "acme", "repo": "demo"}, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := callToolRaw(t, cs, tt.name, tt.args)
			items := listStructuredItems(t, res)
			if len(items) != tt.want {
				t.Errorf("items length = %d, want %d", len(items), tt.want)
			}
		})
	}
}

// TestObjectReturningToolsRemainObjects verifies that single-object tools
// still return a JSON object in StructuredContent and are not accidentally
// wrapped by the list fix.
func TestObjectReturningToolsRemainObjects(t *testing.T) {
	cs, _ := setup(t)

	tests := []struct {
		name string
		args map[string]any
	}{
		{name: "forgejo_repo_get", args: map[string]any{"owner": "acme", "repo": "demo"}},
		{name: "forgejo_file_get", args: map[string]any{"owner": "acme", "repo": "demo", "path": "main.go"}},
		{name: "forgejo_issue_get", args: map[string]any{"owner": "acme", "repo": "demo", "index": 5}},
		{name: "forgejo_pull_get", args: map[string]any{"owner": "acme", "repo": "demo", "number": 5}},
		{name: "forgejo_diff_get", args: map[string]any{"owner": "acme", "repo": "demo", "basehead": "main..dev"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := callToolRaw(t, cs, tt.name, tt.args)
			if _, ok := res.StructuredContent.(map[string]any); !ok {
				t.Fatalf("StructuredContent is %T, want map[string]any (object), value=%v", res.StructuredContent, res.StructuredContent)
			}
		})
	}
}

// TestListToolsEmptyItems verifies that a list tool returning no rows still
// yields an object with an empty "items" array (never a bare empty array).
func TestListToolsEmptyItems(t *testing.T) {
	cs, _ := setup(t)
	// forgejo_branch_list on acme/empty returns an empty JSON array from the
	// mock; it must still be surfaced as an object with an empty "items" array.
	res := callToolRaw(t, cs, "forgejo_branch_list", map[string]any{"owner": "acme", "repo": "empty"})
	items := listStructuredItems(t, res)
	if len(items) != 0 {
		t.Fatalf("items length = %d, want 0", len(items))
	}
}
