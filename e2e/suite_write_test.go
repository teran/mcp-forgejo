//go:build e2e

package e2e

import (
	"encoding/base64"
)

// repoDetailResult is the repository shape returned by repo_update/repo_get
// (includes description, which the minimal repoResult omits).
type repoDetailResult struct {
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Description   string `json:"description"`
}

// TestWriteRepoCreate creates and deletes a throwaway repository.
func (s *FullSuite) TestWriteRepoCreate() {
	t := s.T()
	name := s.ns + "-wrepo"
	repo := callSuiteJSON[repoResult](s, t, "forgejo_repo_create", map[string]any{
		"name": name, "auto_init": true, "default_branch": "main",
	})
	s.Assert().Equal(name, repo.Name)
	s.Assert().Equal("main", repo.DefaultBranch)
	s.stand.Defer(t, func() {
		CallJSON[any](s.stand, t, "forgejo_repo_delete", map[string]any{
			"owner": s.stand.Admin(), "repo": name, "confirm": true,
		})
	})
}

// TestWriteOrgCreate creates and deletes a throwaway organization.
func (s *FullSuite) TestWriteOrgCreate() {
	t := s.T()
	name := s.ns + "-worg"
	org := callSuiteJSON[orgResult](s, t, "forgejo_org_create", map[string]any{
		"username": name, "full_name": "Write Org",
	})
	s.Assert().Equal(name, org.Username)
	s.stand.Defer(t, func() {
		CallJSON[any](s.stand, t, "forgejo_org_delete", map[string]any{"org": name})
	})
}

// TestWriteFileWriteMany writes several files in one commit and asserts a
// single commit is produced covering all of them. Non-idempotent.
func (s *FullSuite) TestWriteFileWriteMany() {
	t := s.T()
	res := callSuiteJSON[changeFilesResult](s, t, "forgejo_file_write_many", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "branch": "main",
		"message": "e2e: multi-file",
		"files": []map[string]any{
			{"path": "a.txt", "content": "aaa", "operation": "create"},
			{"path": "b.txt", "content": "bbb", "operation": "create"},
		},
	})
	s.Assert().NotEmpty(res.CommitSHA)
	s.Assert().Len(res.Files, 2)
	paths := map[string]bool{}
	for _, f := range res.Files {
		paths[f.Path] = true
		s.Assert().NotEmpty(f.SHA)
	}
	s.Assert().True(paths["a.txt"])
	s.Assert().True(paths["b.txt"])

	// Confirm both files are present in the repo.
	contents := callSuiteJSON[contentsResult](s, t, "forgejo_repo_list_contents", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName,
	})
	names := map[string]bool{}
	for _, e := range contents.Items {
		names[e.Name] = true
	}
	s.Assert().True(names["a.txt"], "a.txt not created: %+v", contents.Items)
	s.Assert().True(names["b.txt"], "b.txt not created: %+v", contents.Items)

	// Clean up the throwaway files (delete is a separate tool; reuse it here to
	// keep the fixture repo tidy).
	s.stand.Defer(t, func() {
		for _, p := range []string{"a.txt", "b.txt"} {
			CallJSON[any](s.stand, t, "forgejo_file_delete", map[string]any{
				"owner": s.stand.Admin(), "repo": s.repoName, "path": p,
				"branch": "main", "message": "e2e: cleanup multi-file",
			})
		}
	})
}

// TestWriteFileWriteManyUpdate covers the `update` operation of
// forgejo_file_write_many, carrying the target file's blob SHA.
//
// CONTRACT FOR @developer (see report): the server's file_write_many input
// schema (fileEntryIn) currently has NO `sha` field, and the wire request
// (changeFilesRequest/changeFileEntryRequest) does not propagate one, so Forgejo
// rejects an `update` with 422 (Forgejo requires the current file SHA to update
// a blob). This test encodes the DESIRED behavior:
//   - create a file (no sha needed),
//   - read its blob SHA via forgejo_file_get,
//   - file_write_many with operation=update + that sha -> a NEW commit that
//     updates the file's content.
//
// Until @developer adds sha support (fileEntryIn.sha -> domain.ChangeFileEntry.SHA
// -> changeFileEntryRequest.SHA), this test is EXPECTED to fail.
func (s *FullSuite) TestWriteFileWriteManyUpdate() {
	t := s.T()
	path := s.ns + "-upd.txt"

	create := callSuiteJSON[changeFilesResult](s, t, "forgejo_file_write_many", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "branch": "main",
		"message": "e2e: many-update create",
		"files": []map[string]any{
			{"path": path, "content": "v1", "operation": "create"},
		},
	})
	s.Assert().NotEmpty(create.CommitSHA)

	f := callSuiteJSON[fileContentResult](s, t, "forgejo_file_get", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "path": path,
	})
	s.Assert().NotEmpty(f.SHA)

	upd := callSuiteJSON[changeFilesResult](s, t, "forgejo_file_write_many", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "branch": "main",
		"message": "e2e: many-update",
		"files": []map[string]any{
			{"path": path, "content": "v2", "operation": "update", "sha": f.SHA},
		},
	})
	s.Assert().NotEmpty(upd.CommitSHA)
	s.Assert().NotEqual(create.CommitSHA, upd.CommitSHA, "update must produce a new commit")

	got := callSuiteJSON[fileContentResult](s, t, "forgejo_file_get", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "path": path,
	})
	s.Assert().Equal("v2", got.Content)
	s.stand.Defer(t, func() {
		CallJSON[any](s.stand, t, "forgejo_file_delete", map[string]any{
			"owner": s.stand.Admin(), "repo": s.repoName, "path": path,
			"branch": "main", "message": "e2e: cleanup many-update",
		})
	})
}

// TestWriteIssueUpdate edits an ISOLATED issue (title/body) and leaves it open.
// Operates on a throwaway issue so the shared fixture's title stays stable
// across the read tests (fragility reduction). Idempotent.
func (s *FullSuite) TestWriteIssueUpdate() {
	t := s.T()
	index := s.newIsolatedIssue(t)
	iss := callSuiteJSON[issueResult](s, t, "forgejo_issue_update", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": index,
		"title": "Isolated issue (updated)", "body": "updated body", "state": "open",
	})
	s.Assert().Equal("Isolated issue (updated)", iss.Title)
	s.Assert().Equal("open", iss.State)
}

// TestWritePullUpdate edits an ISOLATED PR title (leaves it open). Idempotent.
func (s *FullSuite) TestWritePullUpdate() {
	t := s.T()
	number := s.newIsolatedPR(t)
	pr := callSuiteJSON[pullResult](s, t, "forgejo_pull_update", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": number,
		"title": "Isolated PR (updated)", "body": "updated", "state": "open",
	})
	s.Assert().Equal("Isolated PR (updated)", pr.Title)
	s.Assert().Equal("open", pr.State)
}

// TestWritePullMerge opens a dedicated PR (unique source branch) and merges it.
func (s *FullSuite) TestWritePullMerge() {
	t := s.T()
	branch := s.ns + "-merge"
	callSuiteJSON[branchResult](s, t, "forgejo_branch_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "new_branch": branch, "old_ref": "main",
	})
	callSuiteJSON[fileResult](s, t, "forgejo_file_write", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "path": "mergefile.md",
		"branch": branch, "message": "e2e: merge file", "content": "# merge\n",
	})
	pr := callSuiteJSON[pullResult](s, t, "forgejo_pull_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "head": branch, "base": "main",
		"title": "Merge PR", "body": "merge me",
	})
	s.Assert().NotZero(pr.Number)

	merged := callSuiteJSON[pullMergeResult](s, t, "forgejo_pull_merge", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": pr.Number, "method": "merge",
	})
	s.Assert().True(merged.Merged)

	// Merging the same PR again reports AlreadyMerged (no error).
	again := callSuiteJSON[pullMergeResult](s, t, "forgejo_pull_merge", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": pr.Number, "method": "merge",
	})
	s.Assert().True(again.AlreadyMerged)

	s.stand.Defer(t, func() {
		CallJSON[any](s.stand, t, "forgejo_branch_delete", map[string]any{
			"owner": s.stand.Admin(), "repo": s.repoName, "branch": branch,
		})
	})
}

// TestWritePullReview reviews an ISOLATED PR using the "comment" event (which
// carries a body), keeping the shared fixture PR free of review mutations.
func (s *FullSuite) TestWritePullReview() {
	t := s.T()
	number := s.newIsolatedPR(t)
	rev := callSuiteJSON[reviewResult](s, t, "forgejo_pull_review", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": number,
		"body": "looks good", "event": "comment",
	})
	s.Assert().NotZero(rev.ID)
	s.Assert().NotEmpty(rev.Body)
}

// TestWriteRepoUpdate edits an ISOLATED repo description. Idempotent.
func (s *FullSuite) TestWriteRepoUpdate() {
	t := s.T()
	repo := s.newIsolatedRepo(t)
	desc := "isolated repo desc " + s.ns
	r := callSuiteJSON[repoDetailResult](s, t, "forgejo_repo_update", map[string]any{
		"owner": s.stand.Admin(), "repo": repo, "description": desc,
	})
	s.Assert().Equal(desc, r.Description)
}

// TestWriteMilestoneUpdate edits an ISOLATED milestone. Idempotent.
func (s *FullSuite) TestWriteMilestoneUpdate() {
	t := s.T()
	id := s.newIsolatedMilestone(t)
	ms := callSuiteJSON[milestoneResult](s, t, "forgejo_milestone_update", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "id": id,
		"title": "Isolated milestone (updated)",
	})
	s.Assert().Equal("Isolated milestone (updated)", ms.Title)
}

// TestWriteLabelUpdate edits an ISOLATED label. Idempotent.
func (s *FullSuite) TestWriteLabelUpdate() {
	t := s.T()
	id := s.newIsolatedLabel(t)
	l := callSuiteJSON[labelResult](s, t, "forgejo_label_update", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "id": id,
		"color": "0e8a16",
	})
	s.Assert().Equal("0e8a16", l.Color)
}

// TestWriteIssueSetLabels replaces an ISOLATED issue's labels with an ISOLATED
// label (idempotent) and asserts it is applied.
func (s *FullSuite) TestWriteIssueSetLabels() {
	t := s.T()
	index := s.newIsolatedIssue(t)
	labelID := s.newIsolatedLabel(t)
	s.callOK(t, "forgejo_issue_set_labels", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": index,
		"labels": []int64{labelID},
	})
	got := callSuiteJSON[issueWithCommentsResult](s, t, "forgejo_issue_get", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": index,
	})
	found := false
	for _, l := range got.Issue.Labels {
		if l.ID == labelID {
			found = true
		}
	}
	s.Assert().True(found, "isolated label not applied: %+v", got.Issue.Labels)
}

// TestWriteReleaseAssetUpload uploads an asset to the fixture release.
// Non-idempotent: two uploads yield two distinct asset IDs.
func (s *FullSuite) TestWriteReleaseAssetUpload() {
	t := s.T()
	content := base64.StdEncoding.EncodeToString([]byte("asset-bytes"))
	a1 := callSuiteJSON[releaseAssetResult](s, t, "forgejo_release_asset_upload", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "release_id": s.releaseID,
		"filename": "notes.txt", "content": content,
	})
	s.Assert().NotZero(a1.ID)
	s.Assert().Equal("notes.txt", a1.Name)
	s.Assert().Equal(int64(11), a1.Size) // len("asset-bytes")

	a2 := callSuiteJSON[releaseAssetResult](s, t, "forgejo_release_asset_upload", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "release_id": s.releaseID,
		"filename": "notes2.txt", "content": content,
	})
	s.Assert().NotZero(a2.ID)
	s.Assert().NotEqual(a1.ID, a2.ID, "each upload must create a distinct asset")
}

// TestWriteReleaseAssetUploadInvalidBase64 drives the validation branch of
// release_asset_upload (invalid base64 content).
func (s *FullSuite) TestWriteReleaseAssetUploadInvalidBase64() {
	t := s.T()
	s.callErr(t, "forgejo_release_asset_upload", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "release_id": s.releaseID,
		"filename": "bad.txt", "content": "!!!not-base64!!!",
	})
}

// TestWriteBranchCreateDuplicate registers coverage for the branch_create happy
// path invoked in fixture setup and proves a duplicate name is rejected
// (409 conflict). The happy path itself is already asserted in SetupSuite.
func (s *FullSuite) TestWriteBranchCreateDuplicate() {
	t := s.T()
	s.callErr(t, "forgejo_branch_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "new_branch": s.devBranch,
		"old_ref": "main",
	})
}

// TestWriteIssueCreateDuplicateTitle registers issue_create coverage and proves
// an empty title is rejected (validation).
func (s *FullSuite) TestWriteIssueCreateValidation() {
	t := s.T()
	s.callErr(t, "forgejo_issue_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "title": "",
	})
}

// TestWritePullCreateDuplicatePR covers pull_create's conflict path with an
// already-open head->base PR.
func (s *FullSuite) TestWritePullCreateDuplicate() {
	t := s.T()
	s.callErr(t, "forgejo_pull_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "head": s.devBranch,
		"base": "main", "title": "Duplicate PR", "body": "dup",
	})
}

// TestWriteCommentAddValidation covers issue_comment_add's empty-body
// validation (the happy path is asserted in SetupSuite).
func (s *FullSuite) TestWriteCommentAddValidation() {
	t := s.T()
	s.callErr(t, "forgejo_issue_comment_add", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "index": s.issueIndex, "body": "",
	})
}

// TestWriteReleaseCreateValidation covers release_create's empty-tag
// validation.
func (s *FullSuite) TestWriteReleaseCreateValidation() {
	t := s.T()
	s.callErr(t, "forgejo_release_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "tag": "", "name": "x",
	})
}

// TestWriteTagCreateValidation covers tag_create's empty-name validation.
func (s *FullSuite) TestWriteTagCreateValidation() {
	t := s.T()
	s.callErr(t, "forgejo_tag_create", map[string]any{
		"owner": s.stand.Admin(), "repo": s.repoName, "name": "", "target": "main",
	})
}

// TestWriteRepoForkValidation covers repo_fork's empty-owner validation.
func (s *FullSuite) TestWriteRepoForkValidation() {
	t := s.T()
	s.callErr(t, "forgejo_repo_fork", map[string]any{
		"owner": "", "repo": s.repoName,
	})
}

// TestWriteRepoCreateEmptyName covers repo_create's empty-name validation.
func (s *FullSuite) TestWriteRepoCreateEmptyName() {
	t := s.T()
	s.callErr(t, "forgejo_repo_create", map[string]any{"name": ""})
}
