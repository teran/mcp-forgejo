//go:build e2e

package e2e

// TestWriteIssueUpdateStateTransitions proves issue_update handles the
// close -> reopen state transitions (not just title/body) on an isolated issue.
func (s *FullSuite) TestWriteIssueUpdateStateTransitions() {
	t := s.T()
	index := s.newIsolatedIssue(t)

	closed := callSuiteJSON[issueResult](s, t, "forgejo_issue_update", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": index, "state": "closed",
	})
	s.Assert().Equal("closed", closed.State)

	reopened := callSuiteJSON[issueResult](s, t, "forgejo_issue_update", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": index, "state": "open",
	})
	s.Assert().Equal("open", reopened.State)
}

// TestWritePullUpdateStateTransitions proves pull_update handles the
// close -> reopen state transitions on an isolated PR.
func (s *FullSuite) TestWritePullUpdateStateTransitions() {
	t := s.T()
	number := s.newIsolatedPR(t)

	closed := callSuiteJSON[pullResult](s, t, "forgejo_pull_update", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": number, "state": "closed",
	})
	s.Assert().Equal("closed", closed.State)

	reopened := callSuiteJSON[pullResult](s, t, "forgejo_pull_update", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": number, "state": "open",
	})
	s.Assert().Equal("open", reopened.State)
}
