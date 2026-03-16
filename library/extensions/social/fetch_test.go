// fetch_test.go - Tests for fetch wrapper functions
package social

import (
	"database/sql"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/fetch"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/core/result"
)

func TestConvertResult_success(t *testing.T) {
	r := fetch.Result{
		Success: true,
		Data: fetch.Stats{
			Repositories: 3,
			Items:        42,
			Errors: []fetch.Error{
				{Repository: "https://github.com/a/b", Error: "timeout"},
			},
		},
	}
	got := convertResult(r)
	if !got.Success {
		t.Fatal("convertResult(success) should succeed")
	}
	if got.Data.Repositories != 3 {
		t.Errorf("Repositories = %d, want 3", got.Data.Repositories)
	}
	if got.Data.Posts != 42 {
		t.Errorf("Posts = %d, want 42", got.Data.Posts)
	}
	if len(got.Data.Errors) != 1 {
		t.Fatalf("Errors len = %d, want 1", len(got.Data.Errors))
	}
	if got.Data.Errors[0].Repository != "https://github.com/a/b" {
		t.Errorf("Error repo = %q", got.Data.Errors[0].Repository)
	}
	if got.Data.Errors[0].Error != "timeout" {
		t.Errorf("Error msg = %q", got.Data.Errors[0].Error)
	}
}

func TestConvertResult_failure(t *testing.T) {
	r := fetch.Result{
		Success: false,
		Error:   &result.Error{Code: "FETCH_FAILED", Message: "network error"},
	}
	got := convertResult(r)
	if got.Success {
		t.Fatal("convertResult(failure) should fail")
	}
	if got.Error.Code != "FETCH_FAILED" {
		t.Errorf("Error.Code = %q", got.Error.Code)
	}
}

func TestConvertResult_noErrors(t *testing.T) {
	r := fetch.Result{
		Success: true,
		Data:    fetch.Stats{Repositories: 1, Items: 5},
	}
	got := convertResult(r)
	if len(got.Data.Errors) != 0 {
		t.Errorf("Errors len = %d, want 0", len(got.Data.Errors))
	}
}

func TestSocialProcessors_returnsSlice(t *testing.T) {
	procs := socialProcessors()
	if len(procs) != 1 {
		t.Errorf("socialProcessors() returned %d, want 1", len(procs))
	}
	if procs[0] == nil {
		t.Error("socialProcessors()[0] should not be nil")
	}
}

func TestSocialHooks_returnsSlice(t *testing.T) {
	hooks := socialHooks()
	if len(hooks) != 3 {
		t.Errorf("socialHooks() returned %d, want 3", len(hooks))
	}
	for i, h := range hooks {
		if h == nil {
			t.Errorf("socialHooks()[%d] should not be nil", i)
		}
	}
}

func TestFetchSocialListRefs_noop(t *testing.T) {
	// fetchSocialListRefs is a no-op; just verify it doesn't panic
	fetchSocialListRefs("", "", "", "")
	fetchSocialListRefs("/tmp", "https://github.com/a/b", "main", "https://github.com/c/d")
}

func TestCheckIfRepoFollowsWorkspace_emptyWorkspace(t *testing.T) {
	// Should return immediately when workspaceURL is empty
	checkIfRepoFollowsWorkspace("/nonexistent", "https://github.com/a/b", "main", "")
}

func TestSyncListToCache(t *testing.T) {
	setupTestDB(t)
	list := List{
		ID:           "test-list",
		Name:         "Test List",
		Version:      "0.1.0",
		Repositories: []string{"https://github.com/a/b#branch:main"},
	}
	// syncListToCache logs warnings but doesn't fail
	syncListToCache(list, "/tmp/test")
}

func TestProcessSocialCommit_withVirtualRefs(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/virtref"
	hash := "aa0012345678"
	branch := "gitmsg/social"
	ts := time.Now().UTC().Format(time.RFC3339)
	header := protocol.Header{Ext: "social", V: "0.1.0", Fields: map[string]string{"type": "post"}}
	refs := []protocol.Ref{{
		Ext:      "social",
		Author:   "Remote",
		Email:    "remote@t.com",
		Time:     ts,
		Ref:      "#commit:ff0011223344@" + branch,
		V:        "0.1.0",
		Fields:   map[string]string{"type": "post"},
		Metadata: "> Quoted content from remote",
	}}
	content := protocol.FormatMessage("A post", header, refs)
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: hash, RepoURL: repoURL, Branch: branch,
		AuthorName: "Test", AuthorEmail: "test@t.com",
		Message: content, Timestamp: time.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	msg := protocol.ParseMessage(content)
	if msg == nil {
		t.Fatal("ParseMessage returned nil")
	}
	gc := git.Commit{Hash: hash, Timestamp: time.Now()}
	processSocialCommit(gc, msg, repoURL, branch)
	count, _ := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_commits WHERE hash = ? AND is_virtual = 1`,
			"ff0011223344").Scan(&c)
		return c, err
	})
	if count != 1 {
		t.Errorf("expected 1 virtual commit, got %d", count)
	}
}

func TestProcessSocialCommit_originalWithLocalRef(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/localref"
	hash := "bb0012345678"
	branch := "gitmsg/social"
	header := protocol.Header{Ext: "social", V: "0.1.0", Fields: map[string]string{
		"type":     "comment",
		"original": "#commit:abc012345678@main",
	}}
	content := protocol.FormatMessage("Nice!", header, nil)
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: hash, RepoURL: repoURL, Branch: branch,
		AuthorName: "Test", AuthorEmail: "test@t.com",
		Message: content, Timestamp: time.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	msg := protocol.ParseMessage(content)
	if msg == nil {
		t.Fatal("ParseMessage returned nil")
	}
	gc := git.Commit{Hash: hash, Timestamp: time.Now()}
	processSocialCommit(gc, msg, repoURL, branch)
	item, err := cache.QueryLocked(func(db *sql.DB) (SocialItem, error) {
		var s SocialItem
		err := db.QueryRow(`SELECT original_repo_url, original_hash, original_branch FROM social_items WHERE repo_url = ? AND hash = ? AND branch = ?`,
			repoURL, hash, branch).Scan(&s.OriginalRepoURL, &s.OriginalHash, &s.OriginalBranch)
		return s, err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !item.OriginalHash.Valid {
		t.Error("OriginalHash should be valid")
	}
}

func TestProcessSocialCommit_replyToRef(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/rplyref"
	hash := "cc0012345678"
	branch := "gitmsg/social"
	header := protocol.Header{Ext: "social", V: "0.1.0", Fields: map[string]string{
		"type":     "comment",
		"original": "#commit:abc012345678@main",
		"reply-to": "#commit:def012345678@main",
	}}
	content := protocol.FormatMessage("Reply", header, nil)
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: hash, RepoURL: repoURL, Branch: branch,
		AuthorName: "Test", AuthorEmail: "test@t.com",
		Message: content, Timestamp: time.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	msg := protocol.ParseMessage(content)
	if msg == nil {
		t.Fatal("ParseMessage returned nil")
	}
	gc := git.Commit{Hash: hash, Timestamp: time.Now()}
	processSocialCommit(gc, msg, repoURL, branch)
	item, err := cache.QueryLocked(func(db *sql.DB) (SocialItem, error) {
		var s SocialItem
		err := db.QueryRow(`SELECT reply_to_repo_url, reply_to_hash FROM social_items WHERE repo_url = ? AND hash = ? AND branch = ?`,
			repoURL, hash, branch).Scan(&s.ReplyToRepoURL, &s.ReplyToHash)
		return s, err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !item.ReplyToHash.Valid {
		t.Error("ReplyToHash should be valid")
	}
}

func TestCheckIfRepoFollowsWorkspace_noLists(t *testing.T) {
	// Non-existent storage dir - should not panic
	checkIfRepoFollowsWorkspace("/nonexistent/path", "https://github.com/a/b", "main", "https://github.com/workspace")
}

func TestSyncListToCache_multipleRepos(t *testing.T) {
	setupTestDB(t)
	list := List{
		ID:      "multi-repo-list",
		Name:    "Multi",
		Version: "0.1.0",
		Repositories: []string{
			"https://github.com/a/b#branch:main",
			"https://github.com/c/d#branch:dev",
		},
	}
	syncListToCache(list, "/tmp/multi")
}

func TestCacheExternalRepoLists_noLists(t *testing.T) {
	// Non-existent storage dir - should not panic
	cacheExternalRepoLists("/nonexistent/path", "https://github.com/a/b", "", "")
}
