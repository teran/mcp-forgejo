package e2e

// TestReadRepositoryGet exercises forgejo_repo_get happy path against the
// fixture repo.
func (s *FullSuite) TestReadRepositoryGet() {
	t := s.T()
	repo := callSuiteJSON[repoResult](s, t, "forgejo_repo_get", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName,
	})
	s.Assert().Equal(s.repoName, repo.Name)
	s.Assert().Equal(s.stand.Admin()+"/"+s.repoName, repo.FullName)
	s.Assert().Equal("main", repo.DefaultBranch)
}

// TestReadRepositoryListContents lists the fixture repo root and asserts the
// seeded README is present.
func (s *FullSuite) TestReadRepositoryListContents() {
	t := s.T()
	contents := callSuiteJSON[contentsResult](s, t, "forgejo_repo_list_contents", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName,
	})
	s.Assert().NotEmpty(contents.Items)
	found := false
	for _, e := range contents.Items {
		if e.Name == "README.md" && e.Type == "file" {
			found = true
		}
	}
	s.Assert().True(found, "README.md not in root: %+v", contents.Items)
}

// TestReadFileGet reads the seeded README and asserts its content round-trips.
func (s *FullSuite) TestReadFileGet() {
	t := s.T()
	f := callSuiteJSON[fileContentResult](s, t, "forgejo_file_get", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "path": "README.md",
	})
	s.Assert().False(f.Binary)
	s.Assert().Contains(f.Content, "# "+s.repoName)
	s.Assert().NotEmpty(f.SHA)
}

// TestReadOrgList asserts the fixture org is listed for the current user.
func (s *FullSuite) TestReadOrgList() {
	t := s.T()
	orgs := callSuiteJSON[orgListResult](s, t, "forgejo_org_list", map[string]any{})
	s.Assert().NotEmpty(orgs.Items)
	found := false
	for _, o := range orgs.Items {
		if o.Username == s.orgName {
			found = true
		}
	}
	s.Assert().True(found, "fixture org %q not listed: %+v", s.orgName, orgs.Items)
}

// TestReadIssueGet reads the fixture issue and asserts its stable identity and
// that its comment is returned. (The title is mutable — idempotency/update
// tests may rename it — so we assert the number, state and comment, not the
// title.)
func (s *FullSuite) TestReadIssueGet() {
	t := s.T()
	got := callSuiteJSON[issueWithCommentsResult](s, t, "forgejo_issue_get", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": s.issueIndex,
	})
	s.Assert().Equal(s.issueIndex, got.Issue.Number)
	s.Assert().Equal("open", got.Issue.State)
	commentFound := false
	for _, c := range got.Comments {
		if c.ID == s.commentID && c.Body == "first comment" {
			commentFound = true
		}
	}
	s.Assert().True(commentFound, "fixture comment not returned: %+v", got.Comments)
}

// TestReadRepoSearch finds the fixture repo by name.
func (s *FullSuite) TestReadRepoSearch() {
	t := s.T()
	repos := callSuiteJSON[repoListResult](s, t, "forgejo_repo_search", map[string]any{
		"q": s.repoName,
	})
	s.Assert().NotEmpty(repos.Items)
	found := false
	for _, r := range repos.Items {
		if r.FullName == s.stand.Admin()+"/"+s.repoName {
			found = true
		}
	}
	s.Assert().True(found, "fixture repo not found by search: %+v", repos.Items)
}

// TestReadDiffGet asserts a divergent diff exists between main and dev.
func (s *FullSuite) TestReadDiffGet() {
	t := s.T()
	diff := callSuiteJSON[diffResult](s, t, "forgejo_diff_get", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "basehead": "main.." + s.devBranch,
	})
	s.Assert().Equal("main.."+s.devBranch, diff.BaseHead)
	s.Assert().Contains(diff.Text, "devfile.md")
}

// TestReadCommitList asserts the fixture repo has commits.
func (s *FullSuite) TestReadCommitList() {
	t := s.T()
	commits := callSuiteJSON[commitListResult](s, t, "forgejo_commit_list", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName,
	})
	s.Assert().NotEmpty(commits.Items)
	for _, c := range commits.Items {
		s.Assert().NotEmpty(c.SHA)
	}
}

// TestReadBranchList asserts both main and dev branches are listed.
func (s *FullSuite) TestReadBranchList() {
	t := s.T()
	branches := callSuiteJSON[branchListResult](s, t, "forgejo_branch_list", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName,
	})
	names := map[string]bool{}
	for _, b := range branches.Items {
		names[b.Name] = true
	}
	s.Assert().True(names["main"], "main missing: %+v", branches.Items)
	s.Assert().True(names[s.devBranch], "dev missing: %+v", branches.Items)
}

// TestReadIssueList asserts the fixture issue is listed as open.
func (s *FullSuite) TestReadIssueList() {
	t := s.T()
	issues := callSuiteJSON[issueListResult](s, t, "forgejo_issue_list", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "state": "open",
	})
	s.Assert().NotEmpty(issues.Items)
	found := false
	for _, iss := range issues.Items {
		if iss.Number == s.issueIndex {
			found = true
		}
	}
	s.Assert().True(found, "fixture issue not listed: %+v", issues.Items)
}

// TestReadPullList asserts the fixture PR is listed as open.
func (s *FullSuite) TestReadPullList() {
	t := s.T()
	prs := callSuiteJSON[pullListResult](s, t, "forgejo_pull_list", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "state": "open",
	})
	s.Assert().NotEmpty(prs.Items)
	found := false
	for _, pr := range prs.Items {
		if pr.Number == s.prNumber {
			found = true
		}
	}
	s.Assert().True(found, "fixture PR not listed: %+v", prs.Items)
}

// TestReadPullGet reads the fixture PR and asserts its changed files.
func (s *FullSuite) TestReadPullGet() {
	t := s.T()
	detail := callSuiteJSON[pullDetailResult](s, t, "forgejo_pull_get", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "number": s.prNumber,
	})
	s.Assert().Equal(s.prNumber, detail.PullRequest.Number)
	s.Assert().NotEmpty(detail.Files)
}

// TestReadReleaseList asserts the fixture release is listed.
func (s *FullSuite) TestReadReleaseList() {
	t := s.T()
	rels := callSuiteJSON[releaseListResult](s, t, "forgejo_release_list", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName,
	})
	s.Assert().NotEmpty(rels.Items)
	found := false
	for _, r := range rels.Items {
		if r.ID == s.releaseID && r.TagName == s.tagName {
			found = true
		}
	}
	s.Assert().True(found, "fixture release not listed: %+v", rels.Items)
}

// TestReadTagList asserts the fixture tag is listed.
func (s *FullSuite) TestReadTagList() {
	t := s.T()
	tags := callSuiteJSON[tagListResult](s, t, "forgejo_tag_list", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName,
	})
	s.Assert().NotEmpty(tags.Items)
	found := false
	for _, tag := range tags.Items {
		if tag.Name == s.tagName {
			found = true
		}
	}
	s.Assert().True(found, "fixture tag not listed: %+v", tags.Items)
}

// TestReadMilestoneList asserts the fixture milestone is listed (by stable ID;
// the title is mutable across update tests).
func (s *FullSuite) TestReadMilestoneList() {
	t := s.T()
	ms := callSuiteJSON[milestoneListResult](s, t, "forgejo_milestone_list", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName,
	})
	s.Assert().NotEmpty(ms.Items)
	found := false
	for _, m := range ms.Items {
		if m.ID == s.milestoneID {
			found = true
		}
	}
	s.Assert().True(found, "fixture milestone not listed: %+v", ms.Items)
}

// TestReadLabelList asserts the fixture label is listed.
func (s *FullSuite) TestReadLabelList() {
	t := s.T()
	labels := callSuiteJSON[labelListResult](s, t, "forgejo_label_list", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName,
	})
	s.Assert().NotEmpty(labels.Items)
	found := false
	for _, l := range labels.Items {
		if l.ID == s.labelID && l.Name == "bug" {
			found = true
		}
	}
	s.Assert().True(found, "fixture label not listed: %+v", labels.Items)
}

// TestReadUserGet returns the current authenticated user.
func (s *FullSuite) TestReadUserGet() {
	t := s.T()
	u := callSuiteJSON[userResult](s, t, "forgejo_user_get", map[string]any{})
	s.Assert().Equal(s.stand.Admin(), u.Login)
}

// TestReadUserList finds the admin user by search.
func (s *FullSuite) TestReadUserList() {
	t := s.T()
	users := callSuiteJSON[userListResult](s, t, "forgejo_user_list", map[string]any{
		"q": s.stand.Admin(),
	})
	s.Assert().NotEmpty(users.Items)
	found := false
	for _, u := range users.Items {
		if u.Login == s.stand.Admin() {
			found = true
		}
	}
	s.Assert().True(found, "admin user not found by search: %+v", users.Items)
}

// TestReadBranchGet fetches the default branch by name. We assert the stable
// properties (name, commit sha); Forgejo's single-branch GET does not set the
// `default` flag reliably.
func (s *FullSuite) TestReadBranchGet() {
	t := s.T()
	b := callSuiteJSON[branchResult](s, t, "forgejo_branch_get", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "branch": "main",
	})
	s.Assert().Equal("main", b.Name)
	s.Assert().NotEmpty(b.CommitSHA)
}

// Helper result types used only by the read tests above.
type (
	repoListResult struct {
		Items []repoResult `json:"items"`
	}
	issueListResult struct {
		Items []issueResult `json:"items"`
	}
	pullListResult struct {
		Items []pullResult `json:"items"`
	}
	fileContentResult struct {
		Content string `json:"content"`
		Binary  bool   `json:"binary"`
		SHA     string `json:"sha"`
	}
)
