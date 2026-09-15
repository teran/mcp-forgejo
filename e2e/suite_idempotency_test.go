//go:build e2e

package e2e

// TestIdempotencyFileWriteNonIdempotent proves file_write records a NEW commit
// on every call with identical content (not idempotent).
func (s *FullSuite) TestIdempotencyFileWriteNonIdempotent() {
	t := s.T()
	path := s.ns + "-noidem.txt"
	a1 := callSuiteJSON[fileResult](s, t, "forgejo_file_write", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "path": path,
		"branch": "main", "message": "e2e: noidem 1", "content": "same",
	})
	a2 := callSuiteJSON[fileResult](s, t, "forgejo_file_write", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "path": path,
		"branch": "main", "message": "e2e: noidem 2", "content": "same",
	})
	s.Assert().NotEqual(a1.CommitSHA, a2.CommitSHA,
		"repeated file_write must produce a new commit")
	s.stand.Defer(t, func() {
		CallJSON[any](s.stand, t, "forgejo_file_delete", map[string]any{
			"owner": s.stand.Admin(), "repo": s.repoName, "path": path,
			"branch": "main", "message": "e2e: cleanup noidem",
		})
	})
}

// TestIdempotencyFileWriteManyNonIdempotent proves file_write_many records a
// NEW commit on every call. (Its `update` operation cannot be exercised here
// because Forgejo requires a file SHA for updates, which the multi-file write
// tool does not carry — a documented server limitation — so we prove
// non-idempotency by showing each call lands a distinct new commit.)
func (s *FullSuite) TestIdempotencyFileWriteManyNonIdempotent() {
	t := s.T()
	r1 := callSuiteJSON[changeFilesResult](s, t, "forgejo_file_write_many", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "branch": "main",
		"message": "e2e: many noidem 1",
		"files": []map[string]any{
			{"path": s.ns + "-m1.txt", "content": "a", "operation": "create"},
		},
	})
	r2 := callSuiteJSON[changeFilesResult](s, t, "forgejo_file_write_many", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "branch": "main",
		"message": "e2e: many noidem 2",
		"files": []map[string]any{
			{"path": s.ns + "-m2.txt", "content": "b", "operation": "create"},
		},
	})
	s.Assert().NotEqual(r1.CommitSHA, r2.CommitSHA,
		"each file_write_many call must produce a new commit")
	s.stand.Defer(t, func() {
		for _, p := range []string{s.ns + "-m1.txt", s.ns + "-m2.txt"} {
			CallJSON[any](s.stand, t, "forgejo_file_delete", map[string]any{
				"owner": s.stand.Admin(), "repo": s.repoName, "path": p,
				"branch": "main", "message": "e2e: cleanup many noidem",
			})
		}
	})
}

// TestIdempotencyCommentAddNonIdempotent proves each comment_add creates a new
// comment.
func (s *FullSuite) TestIdempotencyCommentAddNonIdempotent() {
	t := s.T()
	c1 := callSuiteJSON[commentResult](s, t, "forgejo_issue_comment_add", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": s.issueIndex,
		"body": "idem-comment",
	})
	c2 := callSuiteJSON[commentResult](s, t, "forgejo_issue_comment_add", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": s.issueIndex,
		"body": "idem-comment",
	})
	s.Assert().NotEqual(c1.ID, c2.ID, "repeated comment_add must create distinct comments")
}

// TestIdempotencyIssueUpdate proves repeating the same issue edit is a no-op
// with no error.
func (s *FullSuite) TestIdempotencyIssueUpdate() {
	t := s.T()
	args := map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": s.issueIndex,
		"title": "Idempotent issue title", "body": "idem", "state": "open",
	}
	first := callSuiteJSON[issueResult](s, t, "forgejo_issue_update", args)
	second := callSuiteJSON[issueResult](s, t, "forgejo_issue_update", args)
	s.Assert().Equal(first.Title, second.Title)
	s.Assert().Equal("open", second.State)
}

// TestIdempotencyPullUpdate proves repeating the same PR edit is a no-op.
func (s *FullSuite) TestIdempotencyPullUpdate() {
	t := s.T()
	args := map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": s.prNumber,
		"title": "Idempotent PR title", "state": "open",
	}
	first := callSuiteJSON[pullResult](s, t, "forgejo_pull_update", args)
	second := callSuiteJSON[pullResult](s, t, "forgejo_pull_update", args)
	s.Assert().Equal(first.Title, second.Title)
	s.Assert().Equal("open", second.State)
}

// TestIdempotencySetLabels proves repeating the same label set is a no-op.
func (s *FullSuite) TestIdempotencySetLabels() {
	t := s.T()
	args := map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": s.issueIndex,
		"labels": []int64{s.labelID},
	}
	s.callOK(t, "forgejo_issue_set_labels", args)
	s.callOK(t, "forgejo_issue_set_labels", args) // second identical call: no error, no change
}

// TestIdempotencyRepoUpdate proves repeating the same repo edit is a no-op.
func (s *FullSuite) TestIdempotencyRepoUpdate() {
	t := s.T()
	args := map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "description": "idempotent desc",
	}
	first := callSuiteJSON[repoDetailResult](s, t, "forgejo_repo_update", args)
	second := callSuiteJSON[repoDetailResult](s, t, "forgejo_repo_update", args)
	s.Assert().Equal(first.Description, second.Description)
}

// TestIdempotencyLabelUpdate proves repeating the same label edit is a no-op.
func (s *FullSuite) TestIdempotencyLabelUpdate() {
	t := s.T()
	args := map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "id": s.labelID, "color": "5319e7",
	}
	first := callSuiteJSON[labelResult](s, t, "forgejo_label_update", args)
	second := callSuiteJSON[labelResult](s, t, "forgejo_label_update", args)
	s.Assert().Equal(first.Color, second.Color)
}

// TestIdempotencyMilestoneUpdate proves repeating the same milestone edit is a
// no-op.
func (s *FullSuite) TestIdempotencyMilestoneUpdate() {
	t := s.T()
	args := map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "id": s.milestoneID,
		"title": "Idempotent milestone",
	}
	first := callSuiteJSON[milestoneResult](s, t, "forgejo_milestone_update", args)
	second := callSuiteJSON[milestoneResult](s, t, "forgejo_milestone_update", args)
	s.Assert().Equal(first.Title, second.Title)
}
