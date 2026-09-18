//go:build e2e

package e2e

// TestReadFileGetRefBranch proves file_get honours the `ref` parameter for a
// branch (not just the default branch): the dev branch carries devfile.md.
func (s *FullSuite) TestReadFileGetRefBranch() {
	t := s.T()
	f := callSuiteJSON[fileContentResult](s, t, "forgejo_file_get", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "path": "devfile.md", "ref": s.devBranch,
	})
	s.Assert().False(f.Binary)
	s.Assert().Contains(f.Content, "# dev")
	s.Assert().NotEmpty(f.SHA)
}

// TestReadFileGetRefSHA proves file_get honours the `ref` parameter for a
// commit SHA: reading README.md at the seed commit returns the seed content.
func (s *FullSuite) TestReadFileGetRefSHA() {
	t := s.T()
	f := callSuiteJSON[fileContentResult](s, t, "forgejo_file_get", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "path": "README.md", "ref": s.mainCommitSHA,
	})
	s.Assert().False(f.Binary)
	s.Assert().Contains(f.Content, "# "+s.repoName)
}

// TestReadListContentsRefBranch proves repo_list_contents honours the `ref`
// parameter for a branch: listing the dev branch root shows devfile.md.
func (s *FullSuite) TestReadListContentsRefBranch() {
	t := s.T()
	contents := callSuiteJSON[contentsResult](s, t, "forgejo_repo_list_contents", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "ref": s.devBranch,
	})
	found := false
	for _, e := range contents.Items {
		if e.Name == "devfile.md" && e.Type == "file" {
			found = true
		}
	}
	s.Assert().True(found, "devfile.md not in dev branch root: %+v", contents.Items)
}

// TestReadDiffGetByPR proves diff_get honours the `pr` parameter (the non-default
// path that fetches a pull request's diff) using the shared fixture PR.
func (s *FullSuite) TestReadDiffGetByPR() {
	t := s.T()
	diff := callSuiteJSON[diffResult](s, t, "forgejo_diff_get", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "pr": s.prNumber,
	})
	s.Assert().NotEmpty(diff.Text, "PR diff must not be empty")
	s.Assert().Contains(diff.Text, "devfile.md")
}

// TestWriteRepoCreateTemplates proves repo_create applies the template fields
// (license, gitignore, readme) and that the resulting repo contains the
// generated LICENSE, .gitignore and README files.
func (s *FullSuite) TestWriteRepoCreateTemplates() {
	t := s.T()
	name := s.ns + "-tplrepo"
	repo := callSuiteJSON[repoResult](s, t, "forgejo_repo_create", map[string]any{
		"name": name, "auto_init": true, "default_branch": "main",
		"license": "MIT", "gitignore": "Go", "readme": "Default",
	})
	s.Assert().Equal(name, repo.Name)
	s.Assert().Equal("main", repo.DefaultBranch)
	s.stand.Defer(t, func() {
		CallJSON[any](s.stand, t, "forgejo_repo_delete", map[string]any{
			"owner": s.stand.Admin(), "repo": name, "confirm": true,
		})
	})

	contents := callSuiteJSON[contentsResult](s, t, "forgejo_repo_list_contents", map[string]any{
		"owner": s.stand.Admin(), "repo": name,
	})
	names := map[string]bool{}
	for _, e := range contents.Items {
		names[e.Name] = true
	}
	s.Assert().True(names["LICENSE"], "MIT license file missing: %+v", contents.Items)
	s.Assert().True(names[".gitignore"], ".gitignore missing: %+v", contents.Items)
	s.Assert().True(names["README.md"], "README missing: %+v", contents.Items)
}

// TestWriteRepoCreateUnderOrg proves repo_create accepts an explicit `owner`
// organization and creates the repo inside it.
func (s *FullSuite) TestWriteRepoCreateUnderOrg() {
	t := s.T()
	name := s.ns + "-orgrepo"
	repo := callSuiteJSON[repoResult](s, t, "forgejo_repo_create", map[string]any{
		"owner": s.orgName, "name": name, "auto_init": true, "default_branch": "main",
	})
	s.Assert().Equal(name, repo.Name)
	s.Assert().Equal(s.orgName+"/"+name, repo.FullName)
	s.stand.Defer(t, func() {
		CallJSON[any](s.stand, t, "forgejo_repo_delete", map[string]any{
			"owner": s.orgName, "repo": name, "confirm": true,
		})
	})
}
