//go:build e2e

package e2e

import "encoding/json"

// TestWritePullReviewApprove drives forgejo_pull_review with the `approve`
// event on a dedicated PR reviewed by a SECOND user. Forgejo forbids the PR
// author from approving/rejecting their own pull ("reject your own pull is not
// allowed"), so a collaborator performs the review. The server must forward the
// body to both the create and submit steps for the approve to succeed.
func (s *FullSuite) TestWritePullReviewApprove() {
	t := s.T()
	number := s.newIsolatedPR(t) // authored by the admin user
	reviewer := s.newSecondUser(t)
	defer reviewer.close()

	res, err := reviewer.call(t, "forgejo_pull_review", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": number,
		"body": "approved by e2e", "event": "approve",
	})
	s.Require().NoError(err)
	s.Require().False(res.IsError, "approve review failed: %s", textOf(res))
	var rev reviewResult
	s.Require().NoError(json.Unmarshal([]byte(textOf(res)), &rev))
	s.Assert().NotZero(rev.ID, "approve review must return a review ID")
	s.Assert().NotEmpty(rev.State, "approve review must report a state")
}

// TestWritePullReviewRequestChanges drives forgejo_pull_review with the
// `request_changes` event on a dedicated PR reviewed by a SECOND user.
func (s *FullSuite) TestWritePullReviewRequestChanges() {
	t := s.T()
	number := s.newIsolatedPR(t)
	reviewer := s.newSecondUser(t)
	defer reviewer.close()

	res, err := reviewer.call(t, "forgejo_pull_review", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": number,
		"body": "needs work", "event": "request_changes",
	})
	s.Require().NoError(err)
	s.Require().False(res.IsError, "request_changes review failed: %s", textOf(res))
	var rev reviewResult
	s.Require().NoError(json.Unmarshal([]byte(textOf(res)), &rev))
	s.Assert().NotZero(rev.ID, "request_changes review must return a review ID")
	s.Assert().NotEmpty(rev.State, "request_changes review must report a state")
}

// TestWritePullMergeSquash merges a dedicated PR with the `squash` method.
func (s *FullSuite) TestWritePullMergeSquash() {
	t := s.T()
	number := s.newIsolatedPR(t)
	merged := callSuiteJSON[pullMergeResult](s, t, "forgejo_pull_merge", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": number, "method": "squash",
	})
	s.Assert().True(merged.Merged, "squash merge must report merged")
	s.Assert().False(merged.AlreadyMerged)
}

// TestWritePullMergeRebase merges a dedicated PR with the `rebase` method.
func (s *FullSuite) TestWritePullMergeRebase() {
	t := s.T()
	number := s.newIsolatedPR(t)
	merged := callSuiteJSON[pullMergeResult](s, t, "forgejo_pull_merge", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": number, "method": "rebase",
	})
	s.Assert().True(merged.Merged, "rebase merge must report merged")
	s.Assert().False(merged.AlreadyMerged)
}
