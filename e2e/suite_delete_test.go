//go:build e2e

package e2e

// TestDeleteFile deletes a throwaway file and confirms it is gone.
func (s *FullSuite) TestDeleteFile() {
	t := s.T()
	path := "todelete.txt"
	callSuiteJSON[fileResult](s, t, "forgejo_file_write", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "path": path,
		"branch": "main", "message": "e2e: seed todelete", "content": "x",
	})
	callSuiteJSON[fileResult](s, t, "forgejo_file_delete", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "path": path,
		"branch": "main", "message": "e2e: delete todelete",
	})
	// The file must be gone.
	s.callErr(t, "forgejo_file_get", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "path": path,
	})
}

// TestDeleteBranch deletes a throwaway branch and confirms it is gone.
func (s *FullSuite) TestDeleteBranch() {
	t := s.T()
	branch := s.ns + "-delbranch"
	callSuiteJSON[branchResult](s, t, "forgejo_branch_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "new_branch": branch, "old_ref": "main",
	})
	s.callOK(t, "forgejo_branch_delete", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "branch": branch,
	})
	// Gone -> branch_get errors.
	s.callErr(t, "forgejo_branch_get", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "branch": branch,
	})
}

// TestDeleteBranchRefusesDefault proves the default branch cannot be deleted.
func (s *FullSuite) TestDeleteBranchRefusesDefault() {
	t := s.T()
	res := s.callErr(t, "forgejo_branch_delete", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "branch": "main",
	})
	s.Assert().Contains(textOf(res), "default")
}

// TestDeleteIssue deletes a throwaway issue and confirms it is gone.
func (s *FullSuite) TestDeleteIssue() {
	t := s.T()
	iss := callSuiteJSON[issueResult](s, t, "forgejo_issue_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "title": "delete me",
	})
	s.Assert().NotZero(iss.Number)
	s.callOK(t, "forgejo_issue_delete", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": iss.Number,
	})
	s.callErr(t, "forgejo_issue_get", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": iss.Number,
	})
}

// TestDeleteComment deletes a throwaway comment and confirms the issue no
// longer reports it.
func (s *FullSuite) TestDeleteComment() {
	t := s.T()
	cm := callSuiteJSON[commentResult](s, t, "forgejo_issue_comment_add", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": s.issueIndex,
		"body": "to delete",
	})
	s.Assert().NotZero(cm.ID)
	s.callOK(t, "forgejo_comment_delete", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "comment_id": cm.ID,
	})
	got := callSuiteJSON[issueWithCommentsResult](s, t, "forgejo_issue_get", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": s.issueIndex,
	})
	for _, c := range got.Comments {
		s.Assert().NotEqual(cm.ID, c.ID, "deleted comment still present")
	}
}

// TestDeleteRelease deletes a throwaway release (its tag remains) and confirms
// the release is no longer listed.
func (s *FullSuite) TestDeleteRelease() {
	t := s.T()
	tag := s.ns + "-reltag"
	callSuiteJSON[tagResult](s, t, "forgejo_tag_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "name": tag, "target": "main",
	})
	rel := callSuiteJSON[releaseResult](s, t, "forgejo_release_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "tag": tag, "name": "rel " + tag,
	})
	s.Assert().NotZero(rel.ID)
	s.callOK(t, "forgejo_release_delete", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "id": rel.ID,
	})
	rels := callSuiteJSON[releaseListResult](s, t, "forgejo_release_list", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName,
	})
	for _, r := range rels.Items {
		s.Assert().NotEqual(rel.ID, r.ID, "deleted release still listed")
	}
}

// TestDeleteRepo refuses without confirm and succeeds with confirm:true.
func (s *FullSuite) TestDeleteRepo() {
	t := s.T()
	name := s.ns + "-delrepo"
	callSuiteJSON[repoResult](s, t, "forgejo_repo_create", map[string]any{
		"name": name, "auto_init": true, "default_branch": "main",
	})
	// Without confirmation -> refused.
	res := s.callErr(t, "forgejo_repo_delete", map[string]any{
		"owner": s.stand.Admin(), "repo": name, "confirm": false,
	})
	s.Assert().Contains(textOf(res), "confirmation")
	// With confirmation -> succeeds.
	s.callOK(t, "forgejo_repo_delete", map[string]any{
		"owner": s.stand.Admin(), "repo": name, "confirm": true,
	})
	// Gone -> repo_get errors.
	s.callErr(t, "forgejo_repo_get", map[string]any{
		"owner": s.stand.Admin(), "repo": name,
	})
	// Re-delete of the removed repo is non-idempotent -> errors.
	s.callErr(t, "forgejo_repo_delete", map[string]any{
		"owner": s.stand.Admin(), "repo": name, "confirm": true,
	})
}

// TestDeleteOrg deletes a throwaway org and confirms it is no longer listed.
func (s *FullSuite) TestDeleteOrg() {
	t := s.T()
	name := s.ns + "-delorg"
	callSuiteJSON[orgResult](s, t, "forgejo_org_create", map[string]any{
		"username": name, "full_name": "Del Org",
	})
	s.callOK(t, "forgejo_org_delete", map[string]any{"org": name})
	orgs := callSuiteJSON[orgListResult](s, t, "forgejo_org_list", map[string]any{})
	for _, o := range orgs.Items {
		s.Assert().NotEqual(name, o.Username, "deleted org still listed")
	}
}

// TestDeleteTag deletes a throwaway tag and confirms it is no longer listed.
func (s *FullSuite) TestDeleteTag() {
	t := s.T()
	tag := s.ns + "-deltag"
	callSuiteJSON[tagResult](s, t, "forgejo_tag_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "name": tag, "target": "main",
	})
	s.callOK(t, "forgejo_tag_delete", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "tag": tag,
	})
	tags := callSuiteJSON[tagListResult](s, t, "forgejo_tag_list", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName,
	})
	for _, tg := range tags.Items {
		s.Assert().NotEqual(tag, tg.Name, "deleted tag still listed")
	}
}

// TestDeleteMilestone deletes a throwaway milestone and confirms it is gone.
func (s *FullSuite) TestDeleteMilestone() {
	t := s.T()
	ms := callSuiteJSON[milestoneResult](s, t, "forgejo_milestone_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "title": "del milestone",
	})
	s.Assert().NotZero(ms.ID)
	s.callOK(t, "forgejo_milestone_delete", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "id": ms.ID,
	})
	all := callSuiteJSON[milestoneListResult](s, t, "forgejo_milestone_list", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName,
	})
	for _, m := range all.Items {
		s.Assert().NotEqual(ms.ID, m.ID, "deleted milestone still listed")
	}
}

// TestDeleteLabel deletes a throwaway label and confirms it is gone.
func (s *FullSuite) TestDeleteLabel() {
	t := s.T()
	l := callSuiteJSON[labelResult](s, t, "forgejo_label_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "name": "deltag-label",
		"color": "d73a4a",
	})
	s.Assert().NotZero(l.ID)
	s.callOK(t, "forgejo_label_delete", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "id": l.ID,
	})
	all := callSuiteJSON[labelListResult](s, t, "forgejo_label_list", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName,
	})
	for _, lb := range all.Items {
		s.Assert().NotEqual(l.ID, lb.ID, "deleted label still listed")
	}
}
