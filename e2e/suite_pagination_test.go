//go:build e2e

package e2e

import "github.com/stretchr/testify/require"

// TestReadPaginationIssues proves issue_list honours page/limit when the result
// set exceeds the limit, using dedicated throwaway issues so the shared fixture
// is untouched.
func (s *FullSuite) TestReadPaginationIssues() {
	t := s.T()
	created := s.newIsolatedIssues(t, 5)

	p1 := callSuiteJSON[issueListResult](s, t, "forgejo_issue_list", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "state": "open", "limit": 2, "page": 1,
	})
	p2 := callSuiteJSON[issueListResult](s, t, "forgejo_issue_list", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "state": "open", "limit": 2, "page": 2,
	})
	require.Len(t, p1.Items, 2, "page 1 must return exactly `limit` items")
	require.Len(t, p2.Items, 2, "page 2 must return exactly `limit` items")

	// Pages must be disjoint.
	seen := map[int64]bool{}
	for _, it := range p1.Items {
		require.Falsef(t, seen[it.Number], "issue %d duplicated across pages", it.Number)
		seen[it.Number] = true
	}
	for _, it := range p2.Items {
		require.Falsef(t, seen[it.Number], "issue %d duplicated across pages", it.Number)
		seen[it.Number] = true
	}

	// Every created issue must appear in a single unrestricted listing.
	all := callSuiteJSON[issueListResult](s, t, "forgejo_issue_list", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "state": "open", "limit": 50,
	})
	allNums := map[int64]bool{}
	for _, it := range all.Items {
		allNums[it.Number] = true
	}
	for _, n := range created {
		require.Truef(t, allNums[n], "isolated issue %d missing from full listing", n)
	}
}

// TestReadPaginationPullRequests proves pull_list honours page/limit with more
// open PRs than the limit (shared fixture PR + 3 dedicated PRs).
func (s *FullSuite) TestReadPaginationPullRequests() {
	t := s.T()
	for i := 0; i < 3; i++ {
		s.newIsolatedPR(t)
	}

	p1 := callSuiteJSON[pullListResult](s, t, "forgejo_pull_list", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "state": "open", "limit": 2, "page": 1,
	})
	p2 := callSuiteJSON[pullListResult](s, t, "forgejo_pull_list", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "state": "open", "limit": 2, "page": 2,
	})
	require.Len(t, p1.Items, 2, "page 1 must return exactly `limit` items")
	require.Len(t, p2.Items, 2, "page 2 must return exactly `limit` items")

	seen := map[int64]bool{}
	for _, pr := range p1.Items {
		require.Falsef(t, seen[pr.Number], "PR %d duplicated across pages", pr.Number)
		seen[pr.Number] = true
	}
	for _, pr := range p2.Items {
		require.Falsef(t, seen[pr.Number], "PR %d duplicated across pages", pr.Number)
		seen[pr.Number] = true
	}
}

// TestReadPaginationCommits proves commit_list honours page/limit. The fixture
// repo's main branch carries at least the auto-init commit and the seeded README
// commit, so a limit of 1 yields distinct commits across pages.
func (s *FullSuite) TestReadPaginationCommits() {
	t := s.T()
	c1 := callSuiteJSON[commitListResult](s, t, "forgejo_commit_list", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "branch": "main", "limit": 1, "page": 1,
	})
	c2 := callSuiteJSON[commitListResult](s, t, "forgejo_commit_list", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "branch": "main", "limit": 1, "page": 2,
	})
	require.Len(t, c1.Items, 1, "page 1 must return exactly `limit` commit")
	require.Len(t, c2.Items, 1, "page 2 must return exactly `limit` commit")
	require.NotEqual(t, c1.Items[0].SHA, c2.Items[0].SHA,
		"commit pages must not overlap")
}
