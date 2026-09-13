package e2e

// TestErrorNotFoundRepo drives forgejo_repo_get against a nonexistent repo
// (SPEC §4.4: 404 -> tool returns a clear not-found result).
func (s *FullSuite) TestErrorNotFoundRepo() {
	t := s.T()
	s.callErr(t, "forgejo_repo_get", map[string]any{
		"owner": s.stand.Admin(), "repo": s.ns + "-missing",
	})
}

// TestErrorNotFoundFile drives forgejo_file_get against a missing file.
func (s *FullSuite) TestErrorNotFoundFile() {
	t := s.T()
	s.callErr(t, "forgejo_file_get", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "path": "does-not-exist.md",
	})
}

// TestErrorNotFoundIssue drives forgejo_issue_get with a nonexistent index.
func (s *FullSuite) TestErrorNotFoundIssue() {
	t := s.T()
	s.callErr(t, "forgejo_issue_get", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": int64(999999),
	})
}

// TestErrorNotFoundBranch drives forgejo_branch_get with a nonexistent branch.
func (s *FullSuite) TestErrorNotFoundBranch() {
	t := s.T()
	s.callErr(t, "forgejo_branch_get", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "branch": "nope",
	})
}

// TestErrorNotFoundContents drives forgejo_repo_list_contents on a missing dir.
func (s *FullSuite) TestErrorNotFoundContents() {
	t := s.T()
	s.callErr(t, "forgejo_repo_list_contents", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "path": "no-such-dir",
	})
}

// TestErrorConflictDuplicateRepo proves repo_create with an existing name is
// rejected (409 conflict).
func (s *FullSuite) TestErrorConflictDuplicateRepo() {
	t := s.T()
	s.callErr(t, "forgejo_repo_create", map[string]any{
		"name": s.repoName, "auto_init": true,
	})
}

// TestErrorConflictDuplicateLabel documents that Forgejo does NOT 409 on a
// duplicate label name (it creates/returns a new label), so we assert the
// stable behavior: the call succeeds and never leaks the token.
func (s *FullSuite) TestErrorConflictDuplicateLabel() {
	t := s.T()
	name := s.ns + "-duplabel"
	callSuiteJSON[labelResult](s, t, "forgejo_label_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "name": name, "color": "d73a4a",
	})
	// Forgejo permits duplicate label names; no conflict is raised.
	callSuiteJSON[labelResult](s, t, "forgejo_label_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "name": name, "color": "d73a4a",
	})
}

// TestErrorConflictDuplicateMilestone documents that Forgejo does NOT 409 on a
// duplicate milestone title (it creates a new milestone), so we assert the
// stable behavior: the call succeeds and never leaks the token.
func (s *FullSuite) TestErrorConflictDuplicateMilestone() {
	t := s.T()
	title := s.ns + "-dupms"
	callSuiteJSON[milestoneResult](s, t, "forgejo_milestone_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "title": title,
	})
	// Forgejo permits duplicate milestone titles; no conflict is raised.
	callSuiteJSON[milestoneResult](s, t, "forgejo_milestone_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "title": title,
	})
}

// TestErrorUnauthorized builds a second client with a bogus PAT and proves
// forgejo_repo_get is refused with an ACCESS_DENIED-style error whose message
// never contains the token (SPEC §4.4 401, S2).
func (s *FullSuite) TestErrorUnauthorized() {
	t := s.T()
	bogus := "bogus-token-" + s.ns
	conn := s.newSecondClient(t, bogus)
	defer conn.close()

	res, err := conn.call(t, "forgejo_repo_get", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName,
	})
	s.Require().NoError(err)
	s.Require().True(res.IsError, "expected 401 IsError for bogus PAT")
	msg := textOf(res)
	s.Require().NotEmpty(msg)
	s.Assert().NotContains(msg, bogus, "bogus token leaked into error")
	s.Assert().NotContains(msg, s.stand.pat, "PAT leaked into error")
}

// TestErrorForbiddenReadonlyScope mints a read:repository-scoped PAT and proves
// reads succeed while writes are forbidden (SPEC §4.4 403, S2).
func (s *FullSuite) TestErrorForbiddenReadonlyScope() {
	t := s.T()
	ro := s.mintToken(t, "e2e-ro-"+s.ns, []string{"read:repository"})
	conn := s.newSecondClient(t, ro)
	defer conn.close()

	// Read is allowed with the readonly scope.
	read, err := conn.call(t, "forgejo_repo_get", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName,
	})
	s.Require().NoError(err)
	s.Require().False(read.IsError, "read should be allowed with read:repository scope")

	// Write is forbidden.
	write, err := conn.call(t, "forgejo_repo_create", map[string]any{
		"name": s.ns + "-roblocked", "auto_init": true,
	})
	s.Require().NoError(err)
	s.Require().True(write.IsError, "expected 403 IsError for write with readonly scope")
	msg := textOf(write)
	s.Require().NotEmpty(msg)
	s.Assert().NotContains(msg, ro, "readonly token leaked into error")
	s.Assert().NotContains(msg, s.stand.pat, "PAT leaked into error")
}

// TestErrorArchived archives a throwaway repo via raw REST and proves a write
// tool is refused with an archived error (SPEC §4.4 423, S2). It then
// unarchives and proves the write succeeds again, isolating the archive as the
// cause.
func (s *FullSuite) TestErrorArchived() {
	t := s.T()
	name := s.ns + "-archived"
	callSuiteJSON[repoResult](s, t, "forgejo_repo_create", map[string]any{
		"name": name, "auto_init": true, "default_branch": "main",
	})
	s.stand.Defer(t, func() {
		s.setArchived(t, name, false)
		CallJSON[any](s.stand, t, "forgejo_repo_delete", map[string]any{
			"owner": s.stand.Admin(), "repo": name, "confirm": true,
		})
	})

	s.setArchived(t, name, true)

	// Write to the archived repo is refused.
	res := s.callErr(t, "forgejo_file_write", map[string]any{
		"owner": s.stand.Admin(), "repo": name, "path": "x.txt",
		"branch": "main", "message": "e2e: archived write", "content": "x",
	})
	s.Assert().NotContains(textOf(res), s.stand.pat)

	// Unarchive and the same write succeeds — proving the block was the archive.
	s.setArchived(t, name, false)
	callSuiteJSON[fileResult](s, t, "forgejo_file_write", map[string]any{
		"owner": s.stand.Admin(), "repo": name, "path": "x.txt",
		"branch": "main", "message": "e2e: after unarchive", "content": "x",
	})
}
