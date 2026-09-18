//go:build e2e

package e2e

import (
	"fmt"
	"testing"
	"time"
)

// The shared FullSuite fixtures (issue, PR, milestone, label, repo description)
// are intentionally mutable; several tests update their title/description/state.
// To eliminate order dependence between those tests and the read tests, every
// mutating test operates on a dedicated, throwaway resource created by the
// helpers below. Each helper registers teardown (delete) on the current test so
// the isolated object is released before the suite's shared fixtures are.

// newIsolatedIssue creates a throwaway issue in the shared fixture repo and
// returns its index, registering its deletion.
func (s *FullSuite) newIsolatedIssue(t *testing.T) int64 {
	t.Helper()
	iss := callSuiteJSON[issueResult](s, t, "forgejo_issue_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "title": s.ns + "-iso-issue",
		"body": "isolated issue",
	})
	s.Require().NotZero(iss.Number)
	s.stand.Defer(t, func() {
		CallJSON[any](s.stand, t, "forgejo_issue_delete", map[string]any{
			"owner": s.stand.Admin(), "repo": s.repoName, "index": iss.Number,
		})
	})
	return iss.Number
}

// newIsolatedPR creates a throwaway source branch + file + pull request in the
// shared fixture repo and returns the PR number, registering branch deletion.
// The file path is unique per call so that a merge of one isolated PR never
// makes a later isolated PR's diff empty (which would break squash/rebase).
func (s *FullSuite) newIsolatedPR(t *testing.T) int64 {
	t.Helper()
	branch := fmt.Sprintf("%s-isopr-%d", s.ns, time.Now().UnixNano())
	callSuiteJSON[branchResult](s, t, "forgejo_branch_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "new_branch": branch, "old_ref": "main",
	})
	filePath := fmt.Sprintf("isopr-%d.md", time.Now().UnixNano())
	callSuiteJSON[fileResult](s, t, "forgejo_file_write", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "path": filePath,
		"branch": branch, "message": "e2e: iso pr file", "content": "# iso pr\n",
	})
	pr := callSuiteJSON[pullResult](s, t, "forgejo_pull_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "head": branch, "base": "main",
		"title": "Isolated PR " + s.ns, "body": "isolated",
	})
	s.Require().NotZero(pr.Number)
	s.stand.Defer(t, func() {
		CallJSON[any](s.stand, t, "forgejo_branch_delete", map[string]any{
			"owner": s.stand.Admin(), "repo": s.repoName, "branch": branch,
		})
	})
	return pr.Number
}

// newIsolatedRepo creates a throwaway repository and returns its name,
// registering its deletion.
func (s *FullSuite) newIsolatedRepo(t *testing.T) string {
	t.Helper()
	name := fmt.Sprintf("%s-isorepo-%d", s.ns, time.Now().UnixNano())
	callSuiteJSON[repoResult](s, t, "forgejo_repo_create", map[string]any{
		"name": name, "auto_init": true, "default_branch": "main",
	})
	s.stand.Defer(t, func() {
		CallJSON[any](s.stand, t, "forgejo_repo_delete", map[string]any{
			"owner": s.stand.Admin(), "repo": name, "confirm": true,
		})
	})
	return name
}

// newIsolatedLabel creates a throwaway label in the shared fixture repo and
// returns its ID, registering its deletion.
func (s *FullSuite) newIsolatedLabel(t *testing.T) int64 {
	t.Helper()
	l := callSuiteJSON[labelResult](s, t, "forgejo_label_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "name": s.ns + "-isolabel",
		"color": "d73a4a",
	})
	s.Require().NotZero(l.ID)
	s.stand.Defer(t, func() {
		CallJSON[any](s.stand, t, "forgejo_label_delete", map[string]any{
			"owner": s.stand.Admin(), "repo": s.repoName, "id": l.ID,
		})
	})
	return l.ID
}

// newIsolatedMilestone creates a throwaway milestone in the shared fixture repo
// and returns its ID, registering its deletion.
func (s *FullSuite) newIsolatedMilestone(t *testing.T) int64 {
	t.Helper()
	ms := callSuiteJSON[milestoneResult](s, t, "forgejo_milestone_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "title": s.ns + "-isoms",
	})
	s.Require().NotZero(ms.ID)
	s.stand.Defer(t, func() {
		CallJSON[any](s.stand, t, "forgejo_milestone_delete", map[string]any{
			"owner": s.stand.Admin(), "repo": s.repoName, "id": ms.ID,
		})
	})
	return ms.ID
}

// newIsolatedIssues creates n throwaway issues and returns their indices.
func (s *FullSuite) newIsolatedIssues(t *testing.T, n int) []int64 {
	t.Helper()
	out := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, s.newIsolatedIssue(t))
	}
	return out
}
