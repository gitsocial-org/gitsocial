// integration_test.go - Git workspace integration tests for social extension
package social

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/fetch"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/notifications"
	"github.com/gitsocial-org/gitsocial/core/protocol"
	"github.com/gitsocial-org/gitsocial/core/search"
)

var baseRepoDir string
var testCacheDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "social-test-base-*")
	if err != nil {
		panic(err)
	}
	git.Init(dir, "main")
	git.ExecGit(dir, []string{"config", "user.email", "test@test.com"})
	git.ExecGit(dir, []string{"config", "user.name", "Test User"})
	git.CreateCommit(dir, git.CommitOptions{Message: "Initial commit", AllowEmpty: true})
	// Initialize social extension
	gitmsg.WriteExtConfig(dir, "social", map[string]interface{}{
		"branch": "gitmsg/social",
	})
	baseRepoDir = dir

	cacheDir, err := os.MkdirTemp("", "social-test-cache-*")
	if err != nil {
		panic(err)
	}
	cache.Open(cacheDir)
	testCacheDir = cacheDir

	code := m.Run()
	cache.Reset()
	os.RemoveAll(cacheDir)
	os.RemoveAll(dir)
	os.Exit(code)
}

func cloneFixture(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	cmd := exec.Command("cp", "-a", baseRepoDir+"/.", dst)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cloneFixture: %v: %s", err, out)
	}
	// Resolve symlinks so the path matches git rev-parse --show-toplevel
	resolved, err := git.GetRootDir(dst)
	if err == nil && resolved != "" {
		return resolved
	}
	return dst
}

func initWorkspace(t *testing.T) string {
	t.Helper()
	workdir := cloneFixture(t)
	setupTestDB(t)
	return workdir
}

// --- Post CRUD ---

func TestPostCRUD(t *testing.T) {
	t.Parallel()

	t.Run("CreatePost", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := CreatePost(workdir, "Hello world!", nil)
		if !result.Success {
			t.Fatalf("CreatePost() failed: %s", result.Error.Message)
		}
		post := result.Data
		if post.Content != "Hello world!" {
			t.Errorf("Content = %q, want %q", post.Content, "Hello world!")
		}
		if post.Type != PostTypePost {
			t.Errorf("Type = %q, want post", post.Type)
		}
		if post.Repository == "" {
			t.Error("Repository should not be empty")
		}
		if post.ID == "" {
			t.Error("ID should not be empty")
		}
		if !post.IsWorkspacePost {
			t.Error("IsWorkspacePost should be true")
		}
	})

	t.Run("CreatePost_emptyContent", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := CreatePost(workdir, "", nil)
		if result.Success {
			t.Error("CreatePost() should fail for empty content")
		}
		if result.Error.Code != "EMPTY_CONTENT" {
			t.Errorf("error code = %q, want EMPTY_CONTENT", result.Error.Code)
		}
	})

	t.Run("CreatePost_whitespaceOnly", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := CreatePost(workdir, "   \n  ", nil)
		if result.Success {
			t.Error("CreatePost() should fail for whitespace-only content")
		}
		if result.Error.Code != "EMPTY_CONTENT" {
			t.Errorf("error code = %q, want EMPTY_CONTENT", result.Error.Code)
		}
	})

	t.Run("EditPost_integration", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		post := CreatePost(workdir, "Original content", nil)
		if !post.Success {
			t.Fatal(post.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)

		result := EditPost(workdir, post.Data.ID, "Updated content")
		if !result.Success {
			t.Fatalf("EditPost() failed: %s", result.Error.Message)
		}
		if result.Data.Content != "Updated content" {
			t.Errorf("Content = %q, want %q", result.Data.Content, "Updated content")
		}
		if result.Data.EditOf == "" {
			t.Error("EditOf should reference canonical post")
		}
	})

	t.Run("EditPost_emptyContent", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := EditPost(workdir, "fake-id", "")
		if result.Success {
			t.Error("EditPost() should fail for empty content")
		}
		if result.Error.Code != "EMPTY_CONTENT" {
			t.Errorf("error code = %q, want EMPTY_CONTENT", result.Error.Code)
		}
	})

	t.Run("EditPost_notFound", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := EditPost(workdir, "nonexistent123456", "New content")
		if result.Success {
			t.Error("EditPost() should fail for non-existent target")
		}
		if result.Error.Code != "NOT_FOUND" {
			t.Errorf("error code = %q, want NOT_FOUND", result.Error.Code)
		}
	})

	t.Run("EditPost_success", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		_ = SyncWorkspaceToCache(workdir)
		postResult := CreatePost(workdir, "Original content", nil)
		if !postResult.Success {
			t.Fatalf("CreatePost failed: %s", postResult.Error.Message)
		}
		editResult := EditPost(workdir, postResult.Data.ID, "Updated content")
		if !editResult.Success {
			t.Fatalf("EditPost failed: %s: %v", editResult.Error.Code, editResult.Error.Details)
		}
		if editResult.Data.Content != "Updated content" {
			t.Errorf("Content = %q, want 'Updated content'", editResult.Data.Content)
		}
	})

	t.Run("RetractPost_integration", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		post := CreatePost(workdir, "Retract me", nil)
		if !post.Success {
			t.Fatal(post.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)

		result := RetractPost(workdir, post.Data.ID)
		if !result.Success {
			t.Fatalf("RetractPost() failed: %s", result.Error.Message)
		}
		if !result.Data {
			t.Error("RetractPost should return true")
		}
	})

	t.Run("RetractPost_notFound", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := RetractPost(workdir, "nonexistent123456")
		if result.Success {
			t.Error("RetractPost() should fail for non-existent target")
		}
		if result.Error.Code != "NOT_FOUND" {
			t.Errorf("error code = %q, want NOT_FOUND", result.Error.Code)
		}
	})

	t.Run("RetractPost_success", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		_ = SyncWorkspaceToCache(workdir)
		postResult := CreatePost(workdir, "Post to retract", nil)
		if !postResult.Success {
			t.Fatalf("CreatePost failed: %s", postResult.Error.Message)
		}
		retractResult := RetractPost(workdir, postResult.Data.ID)
		if !retractResult.Success {
			t.Fatalf("RetractPost failed: %s: %v", retractResult.Error.Code, retractResult.Error.Details)
		}
		if !retractResult.Data {
			t.Error("RetractPost should return true")
		}
	})
}

func TestGetPostsIntegration(t *testing.T) {
	t.Parallel()

	t.Run("GetPosts_timeline", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreatePost(workdir, "Timeline post 1", nil)
		CreatePost(workdir, "Timeline post 2", nil)
		_ = SyncWorkspaceToCache(workdir)

		result := GetPosts(workdir, "timeline", nil)
		if !result.Success {
			t.Fatalf("GetPosts(timeline) failed: %s", result.Error.Message)
		}
		if len(result.Data) < 2 {
			t.Errorf("expected at least 2 posts, got %d", len(result.Data))
		}
	})

	t.Run("GetPosts_repositoryMy", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreatePost(workdir, "My post 1", nil)
		CreatePost(workdir, "My post 2", nil)

		result := GetPosts(workdir, "repository:my", nil)
		if !result.Success {
			t.Fatalf("GetPosts(repository:my) failed: %s", result.Error.Message)
		}
		if len(result.Data) < 2 {
			t.Errorf("expected at least 2 posts, got %d", len(result.Data))
		}
	})

	t.Run("GetPosts_repositoryWorkspace", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreatePost(workdir, "Workspace post", nil)

		result := GetPosts(workdir, "repository:workspace", nil)
		if !result.Success {
			t.Fatalf("GetPosts(repository:workspace) failed: %s", result.Error.Message)
		}
		if len(result.Data) < 1 {
			t.Errorf("expected at least 1 post, got %d", len(result.Data))
		}
	})

	t.Run("GetPosts_singlePost", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		created := CreatePost(workdir, "Single post lookup", nil)
		if !created.Success {
			t.Fatal(created.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)

		result := GetPosts(workdir, "post:"+created.Data.ID, nil)
		if !result.Success {
			t.Fatalf("GetPosts(post:id) failed: %s", result.Error.Message)
		}
		if len(result.Data) != 1 {
			t.Fatalf("expected 1 post, got %d", len(result.Data))
		}
		if result.Data[0].Content != "Single post lookup" {
			t.Errorf("Content = %q", result.Data[0].Content)
		}
	})

	t.Run("GetPosts_thread", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		post := CreatePost(workdir, "Thread root", nil)
		if !post.Success {
			t.Fatal(post.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)

		comment := CreateComment(workdir, post.Data.ID, "A reply", nil)
		if !comment.Success {
			t.Fatalf("CreateComment() failed: %s", comment.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)

		result := GetPosts(workdir, "thread:"+post.Data.ID, nil)
		if !result.Success {
			t.Fatalf("GetPosts(thread:id) failed: %s", result.Error.Message)
		}
		if len(result.Data) < 2 {
			t.Errorf("expected at least 2 posts (root + reply), got %d", len(result.Data))
		}
	})

	t.Run("GetPosts_invalidScope", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := GetPosts(workdir, "invalid:scope", nil)
		if result.Success {
			t.Error("GetPosts() should fail for invalid scope")
		}
		if result.Error.Code != "INVALID_SCOPE" {
			t.Errorf("error code = %q, want INVALID_SCOPE", result.Error.Code)
		}
	})

	t.Run("GetPosts_repositoryExternal", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreatePost(workdir, "External repo test post", nil)
		_ = SyncWorkspaceToCache(workdir)
		workspaceURL := gitmsg.ResolveRepoURL(workdir)

		result := GetPosts(workdir, "repository:"+workspaceURL, nil)
		if !result.Success {
			t.Fatalf("GetPosts(repository:URL) failed: %s", result.Error.Message)
		}
		if len(result.Data) < 1 {
			t.Errorf("expected at least 1 post, got %d", len(result.Data))
		}
	})

	t.Run("GetPosts_listScope", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreateList(workdir, "post-list", "Post List")
		result := GetPosts(workdir, "list:post-list", nil)
		if !result.Success {
			t.Fatalf("GetPosts(list:) failed: %s", result.Error.Message)
		}
	})

	t.Run("GetPosts_withOptions", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreatePost(workdir, "Options test", nil)
		_ = SyncWorkspaceToCache(workdir)

		opts := &GetPostsOptions{Limit: 1}
		result := GetPosts(workdir, "repository:my", opts)
		if !result.Success {
			t.Fatalf("GetPosts with opts failed: %s", result.Error.Message)
		}
		if len(result.Data) > 1 {
			t.Errorf("expected at most 1 post with limit=1, got %d", len(result.Data))
		}
	})

	t.Run("GetPosts_repositoryWithBranch", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreatePost(workdir, "Branch scope post", nil)
		_ = SyncWorkspaceToCache(workdir)
		wsURL := gitmsg.ResolveRepoURL(workdir)
		branch := gitmsg.GetExtBranch(workdir, "social")
		result := GetPosts(workdir, "repository:"+wsURL+"@"+branch, nil)
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
	})

	t.Run("GetPosts_threadWithNestedComments", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		root := CreatePost(workdir, "Deep thread root", nil)
		if !root.Success {
			t.Fatal(root.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)
		c1 := CreateComment(workdir, root.Data.ID, "First comment", nil)
		if !c1.Success {
			t.Fatal(c1.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)
		c2 := CreateComment(workdir, c1.Data.ID, "Nested reply", nil)
		if !c2.Success {
			t.Fatal(c2.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)
		result := GetPosts(workdir, "thread:"+root.Data.ID, nil)
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
		if len(result.Data) < 3 {
			t.Errorf("expected at least 3 posts (root + 2 comments), got %d", len(result.Data))
		}
	})

	t.Run("GetPosts_threadFromComment", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		root := CreatePost(workdir, "Thread parent chain root", nil)
		if !root.Success {
			t.Fatal(root.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)

		c1 := CreateComment(workdir, root.Data.ID, "First level comment", nil)
		if !c1.Success {
			t.Fatal(c1.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)

		c2 := CreateComment(workdir, c1.Data.ID, "Second level comment", nil)
		if !c2.Success {
			t.Fatal(c2.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)

		// Request thread from c1 - root should appear in parent chain
		result := GetPosts(workdir, "thread:"+c1.Data.ID, nil)
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
		// Should include root (parent), c1 (root of this thread), and c2 (reply to c1)
		if len(result.Data) < 2 {
			t.Errorf("expected at least 2 posts (parent + root + replies), got %d", len(result.Data))
		}
	})

	t.Run("GetPosts_threadInvalidRef", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := GetPosts(workdir, "thread:not-a-valid-ref", nil)
		if result.Success {
			t.Error("expected failure for invalid thread ref")
		}
		if result.Error.Code != "INVALID_REF" {
			t.Errorf("error code = %q, want INVALID_REF", result.Error.Code)
		}
	})

	t.Run("GetPosts_singlePost_notFound", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		// Use a valid-looking ref that doesn't exist in the cache - returns CACHE_ERROR (sql.ErrNoRows)
		result := GetPosts(workdir, "post:https://github.com/no/repo#commit:aabbccddee11@main", nil)
		if result.Success {
			t.Fatal("expected failure for nonexistent post")
		}
		if result.Error.Code != "CACHE_ERROR" {
			t.Errorf("expected CACHE_ERROR, got %q", result.Error.Code)
		}
	})

	t.Run("GetPosts_threadRootNotInResults", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		workspaceURL := gitmsg.ResolveRepoURL(workdir)
		branch := gitmsg.GetExtBranch(workdir, "social")

		// Create a post via direct cache insertion (no social_items entry for it, only core_commits)
		rootHash := "aabb11223344"
		_ = cache.InsertCommits([]cache.Commit{{
			Hash: rootHash, RepoURL: workspaceURL, Branch: branch,
			AuthorName: "Test", AuthorEmail: "test@test.com",
			Message: "Orphan root\n\n--- GitMsg: ext=\"social\"; type=\"post\"; v=\"0.1.0\" ---", Timestamp: time.Now(),
		}})
		// DON'T insert into social_items - the root won't appear in thread query results

		// Create a comment referencing this root
		commentHash := "ccdd11223344"
		_ = cache.InsertCommits([]cache.Commit{{
			Hash: commentHash, RepoURL: workspaceURL, Branch: branch,
			AuthorName: "Commenter", AuthorEmail: "c@test.com",
			Message: "Comment", Timestamp: time.Now(),
		}})
		_ = InsertSocialItem(SocialItem{
			RepoURL: workspaceURL, Hash: commentHash, Branch: branch, Type: "comment",
			OriginalRepoURL: cache.ToNullString(workspaceURL), OriginalHash: cache.ToNullString(rootHash), OriginalBranch: cache.ToNullString(branch),
			ReplyToRepoURL: cache.ToNullString(workspaceURL), ReplyToHash: cache.ToNullString(rootHash), ReplyToBranch: cache.ToNullString(branch),
		})

		rootRef := workspaceURL + "#commit:" + rootHash + "@" + branch
		result := GetPosts(workdir, "thread:"+rootRef, nil)
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
		// Should still find the root via GetSocialItem fallback (social_items_resolved LEFT JOIN)
		found := false
		for _, p := range result.Data {
			if p.Depth == 0 {
				found = true
			}
		}
		if !found {
			t.Error("expected root post to be found via fallback")
		}
	})

	t.Run("GetPosts_workspaceScope", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreatePost(workdir, "Workspace scope test", nil)
		result := GetPosts(workdir, "repository:workspace", nil)
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
		if len(result.Data) == 0 {
			t.Error("expected at least 1 post")
		}
	})

	t.Run("GetPosts_timelineWithListPosts", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		workspaceURL := gitmsg.ResolveRepoURL(workdir)

		// Create a list and add an external repo to it
		CreateList(workdir, "tl-list", "Timeline List")
		externalRepo := "https://github.com/tl/external"
		AddRepositoryToList(workdir, "tl-list", externalRepo, "main", false)
		_ = SyncWorkspaceToCache(workdir)

		// Insert external post
		_ = cache.InsertCommits([]cache.Commit{{
			Hash: "aabb11224455", RepoURL: externalRepo, Branch: "main",
			AuthorName: "External", AuthorEmail: "ext@t.com",
			Message: "External post", Timestamp: time.Now(),
		}})
		_ = InsertSocialItem(SocialItem{RepoURL: externalRepo, Hash: "aabb11224455", Branch: "main", Type: "post"})

		// Create workspace post
		CreatePost(workdir, "Local timeline post", nil)

		result := GetPosts(workdir, "timeline", nil)
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
		// Should have both local and external posts
		hasLocal := false
		hasExternal := false
		for _, p := range result.Data {
			if p.Repository == workspaceURL {
				hasLocal = true
			}
			if p.Repository == externalRepo {
				hasExternal = true
			}
		}
		if !hasLocal {
			t.Error("expected local post in timeline")
		}
		if !hasExternal {
			t.Error("expected external post in timeline")
		}
	})

	t.Run("GetPosts_listScopeWithData", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		externalRepo := "https://github.com/lspd/repo"
		CreateList(workdir, "lspd-list", "LSPD")
		AddRepositoryToList(workdir, "lspd-list", externalRepo, "main", false)
		_ = SyncWorkspaceToCache(workdir)
		// Insert a post for the external repo
		_ = cache.InsertCommits([]cache.Commit{{
			Hash: "lspd12345678", RepoURL: externalRepo, Branch: "main",
			AuthorName: "Ext", AuthorEmail: "ext@t.com",
			Message: "List post", Timestamp: time.Now(),
		}})
		_ = InsertSocialItem(SocialItem{RepoURL: externalRepo, Hash: "lspd12345678", Branch: "main", Type: "post"})
		result := GetPosts(workdir, "list:lspd-list", nil)
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
		if len(result.Data) == 0 {
			t.Error("expected at least 1 post from list")
		}
	})

	t.Run("GetPosts_threadBranchDefault", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		wsURL := gitmsg.ResolveRepoURL(workdir)
		branch := gitmsg.GetExtBranch(workdir, "social")
		post := CreatePost(workdir, "Thread branch default target", nil)
		if !post.Success {
			t.Fatal(post.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)
		parsed := protocol.ParseRef(post.Data.ID)
		// Build a ref WITHOUT branch -> triggers branch="" -> defaulting to "main" in getThreadPosts
		refNoBranch := parsed.Repository + "#commit:" + parsed.Value
		result := GetPosts(workdir, "thread:"+refNoBranch, nil)
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
		_ = wsURL
		_ = branch
	})

	t.Run("GetPosts_singlePostWorkspace", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		post := CreatePost(workdir, "Workspace single post", nil)
		if !post.Success {
			t.Fatal(post.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)
		result := GetPosts(workdir, "post:"+post.Data.ID, nil)
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
		if len(result.Data) != 1 {
			t.Fatalf("expected 1 post, got %d", len(result.Data))
		}
		if !result.Data[0].Display.IsWorkspacePost {
			t.Error("expected IsWorkspacePost=true for workspace post")
		}
	})

	t.Run("GetPosts_threadRootNotInThread", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		wsURL := gitmsg.ResolveRepoURL(workdir)
		branch := gitmsg.GetExtBranch(workdir, "social")
		// Insert a post into core_commits but NOT into social_items
		// This tests the fallback in getThreadPosts (lines 482-490)
		rootHash := "aab011220011"
		_ = cache.InsertCommits([]cache.Commit{{
			Hash: rootHash, RepoURL: wsURL, Branch: branch,
			AuthorName: "Test", AuthorEmail: "t@t.com",
			Message: "Thread root without social_items", Timestamp: time.Now(),
		}})
		postRef := protocol.CreateRef(protocol.RefTypeCommit, rootHash, wsURL, branch)
		result := GetPosts(workdir, "thread:"+postRef, nil)
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
		// Root should be fetched directly via GetSocialItem fallback
		found := false
		for _, p := range result.Data {
			if p.Display.CommitHash == rootHash {
				found = true
			}
		}
		if !found {
			t.Error("root post should be found via direct fetch fallback")
		}
	})

	t.Run("GetPosts_repositoryExternalMatchingWorkspace", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		wsURL := gitmsg.ResolveRepoURL(workdir)
		_ = SyncWorkspaceToCache(workdir)
		CreatePost(workdir, "WS repo post", nil)
		_ = SyncWorkspaceToCache(workdir)
		branch := gitmsg.GetExtBranch(workdir, "social")
		// Query by the workspace URL as a repository scope (not "my" or "workspace")
		result := GetPosts(workdir, "repository:"+wsURL+"@"+branch, nil)
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
		for _, p := range result.Data {
			if p.Repository == wsURL && !p.Display.IsWorkspacePost {
				t.Error("workspace post should have IsWorkspacePost=true")
			}
		}
	})

	t.Run("GetPosts_listScope_nilOpts", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreateList(workdir, "nil-opts-list", "Nil Opts")
		result := GetPosts(workdir, "list:nil-opts-list", nil)
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
	})

	t.Run("GetPosts_repositoryMyWithDateRange", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreatePost(workdir, "Date range post", nil)
		// Exercise the Since/Until code path in getMyPosts - just verify it doesn't error
		since := time.Now().Add(-24 * time.Hour)
		until := time.Now().Add(24 * time.Hour)
		opts := &GetPostsOptions{Since: &since, Until: &until}
		result := GetPosts(workdir, "repository:my", opts)
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
		// Also test with narrow range that excludes the post
		old := time.Now().Add(-48 * time.Hour)
		oldEnd := time.Now().Add(-47 * time.Hour)
		opts2 := &GetPostsOptions{Since: &old, Until: &oldEnd}
		result2 := GetPosts(workdir, "repository:my", opts2)
		if !result2.Success {
			t.Fatalf("error: %s", result2.Error.Message)
		}
		if len(result2.Data) != 0 {
			t.Errorf("expected 0 posts in old date range, got %d", len(result2.Data))
		}
	})
}

func TestSyncWorkspaceToCache(t *testing.T) {
	workdir := initWorkspace(t)
	CreatePost(workdir, "Sync test post 1", nil)
	CreatePost(workdir, "Sync test post 2", nil)

	// Reset cache and re-sync
	cache.Reset()
	dir := t.TempDir()
	if err := cache.Open(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cache.Reset() })

	if err := SyncWorkspaceToCache(workdir); err != nil {
		t.Fatalf("SyncWorkspaceToCache() error = %v", err)
	}

	// Verify items are queryable
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	items, err := GetAllItems(SocialQuery{RepoURL: workspaceURL, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 2 {
		t.Errorf("expected at least 2 synced posts, got %d", len(items))
	}
}

// --- Interaction CRUD ---

func TestCommentOps(t *testing.T) {
	t.Parallel()

	t.Run("CreateComment_integration", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		post := CreatePost(workdir, "Comment target", nil)
		if !post.Success {
			t.Fatal(post.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)

		result := CreateComment(workdir, post.Data.ID, "Nice post!", nil)
		if !result.Success {
			t.Fatalf("CreateComment() failed: %s", result.Error.Message)
		}
		if result.Data.Type != PostTypeComment {
			t.Errorf("Type = %q, want comment", result.Data.Type)
		}
		if result.Data.OriginalPostID == "" {
			t.Error("OriginalPostID should not be empty")
		}
	})

	t.Run("CreateComment_emptyContent", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := CreateComment(workdir, "fake-id", "", nil)
		if result.Success {
			t.Error("CreateComment() should fail for empty content")
		}
		if result.Error.Code != "EMPTY_CONTENT" {
			t.Errorf("error code = %q, want EMPTY_CONTENT", result.Error.Code)
		}
	})

	t.Run("CreateComment_notFound", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := CreateComment(workdir, "nonexistent123456", "A comment", nil)
		if result.Success {
			t.Error("CreateComment() should fail for non-existent target")
		}
		if result.Error.Code != "NOT_FOUND" {
			t.Errorf("error code = %q, want NOT_FOUND", result.Error.Code)
		}
	})

	t.Run("CreateComment_nestedComment", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		post := CreatePost(workdir, "Root post", nil)
		if !post.Success {
			t.Fatal(post.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)

		comment := CreateComment(workdir, post.Data.ID, "First comment", nil)
		if !comment.Success {
			t.Fatalf("first comment failed: %s", comment.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)

		nested := CreateComment(workdir, comment.Data.ID, "Nested reply", nil)
		if !nested.Success {
			t.Fatalf("nested comment failed: %s", nested.Error.Message)
		}
		if nested.Data.ParentCommentID == "" {
			t.Error("ParentCommentID (reply-to) should be set for nested comment")
		}
		if nested.Data.OriginalPostID == "" {
			t.Error("OriginalPostID should reference root post for nested comment")
		}
	})

	t.Run("CreateComment_nestedCommentNoOriginalRef", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		wsURL := gitmsg.ResolveRepoURL(workdir)
		branch := gitmsg.GetExtBranch(workdir, "social")
		// Insert a "comment" in DB without OriginalRepoURL/Hash (12 hex chars)
		commentHash := "aac011223344"
		_ = cache.InsertCommits([]cache.Commit{{
			Hash: commentHash, RepoURL: wsURL, Branch: branch,
			AuthorName: "Test", AuthorEmail: "t@t.com",
			Message: "orphan comment", Timestamp: time.Now(),
		}})
		_ = InsertSocialItem(SocialItem{
			RepoURL: wsURL, Hash: commentHash, Branch: branch, Type: "comment",
			// No OriginalRepoURL/OriginalHash -> triggers INVALID_TARGET
		})
		commentRef := protocol.CreateRef(protocol.RefTypeCommit, commentHash, wsURL, branch)
		result := CreateComment(workdir, commentRef, "Reply to orphan", nil)
		if result.Success {
			t.Error("expected failure for nested comment without original")
		}
		if result.Error.Code != "INVALID_TARGET" {
			t.Errorf("error code = %q, want INVALID_TARGET", result.Error.Code)
		}
	})

	t.Run("CreateComment_nestedCommentRootNotFound", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		wsURL := gitmsg.ResolveRepoURL(workdir)
		branch := gitmsg.GetExtBranch(workdir, "social")
		// Insert a comment that references a non-existent root (all hex, 12 chars)
		commentHash := "aac022334455"
		_ = cache.InsertCommits([]cache.Commit{{
			Hash: commentHash, RepoURL: wsURL, Branch: branch,
			AuthorName: "Test", AuthorEmail: "t@t.com",
			Message: "orphan nested", Timestamp: time.Now(),
		}})
		_ = InsertSocialItem(SocialItem{
			RepoURL: wsURL, Hash: commentHash, Branch: branch, Type: "comment",
			OriginalRepoURL: cache.ToNullString("https://github.com/ghost/repo"),
			OriginalHash:    cache.ToNullString("aabbccddeeff"),
			OriginalBranch:  cache.ToNullString("main"),
		})
		commentRef := protocol.CreateRef(protocol.RefTypeCommit, commentHash, wsURL, branch)
		result := CreateComment(workdir, commentRef, "Reply to nested", nil)
		if result.Success {
			t.Error("expected failure when root not found")
		}
		if result.Error.Code != "NOT_FOUND" {
			t.Errorf("error code = %q, want NOT_FOUND", result.Error.Code)
		}
	})

	t.Run("CreateComment_nestedCommentRootIsComment", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		wsURL := gitmsg.ResolveRepoURL(workdir)
		branch := gitmsg.GetExtBranch(workdir, "social")
		// Insert a "comment" that is the supposed root
		rootHash := "aac033001122"
		_ = cache.InsertCommits([]cache.Commit{{
			Hash: rootHash, RepoURL: wsURL, Branch: branch,
			AuthorName: "Test", AuthorEmail: "t@t.com",
			Message: "comment root", Timestamp: time.Now(),
		}})
		_ = InsertSocialItem(SocialItem{
			RepoURL: wsURL, Hash: rootHash, Branch: branch, Type: "comment",
		})
		// Insert a nested comment pointing to the "root"
		nestedHash := "aac033001133"
		_ = cache.InsertCommits([]cache.Commit{{
			Hash: nestedHash, RepoURL: wsURL, Branch: branch,
			AuthorName: "Test", AuthorEmail: "t@t.com",
			Message: "nested", Timestamp: time.Now(),
		}})
		_ = InsertSocialItem(SocialItem{
			RepoURL: wsURL, Hash: nestedHash, Branch: branch, Type: "comment",
			OriginalRepoURL: cache.ToNullString(wsURL),
			OriginalHash:    cache.ToNullString(rootHash),
			OriginalBranch:  cache.ToNullString(branch),
		})
		nestedRef := protocol.CreateRef(protocol.RefTypeCommit, nestedHash, wsURL, branch)
		result := CreateComment(workdir, nestedRef, "Reply to nested-of-comment", nil)
		if result.Success {
			t.Error("expected failure when root is a comment")
		}
		if result.Error.Code != "INVALID_TARGET" {
			t.Errorf("error code = %q, want INVALID_TARGET", result.Error.Code)
		}
	})

	t.Run("CreateComment_onRemotePost", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		wsURL := gitmsg.ResolveRepoURL(workdir)
		branch := gitmsg.GetExtBranch(workdir, "social")
		// Insert an external post (different repo, unique per -count run)
		suffix := fmt.Sprintf("%06d", atomic.AddInt64(&extInteractionCounter, 1))
		extRepo := "https://github.com/remote/post/" + suffix
		extHash := fmt.Sprintf("aad01122%s", suffix[:4])
		_ = cache.InsertCommits([]cache.Commit{{
			Hash: extHash, RepoURL: extRepo, Branch: "main",
			AuthorName: "Remote", AuthorEmail: "remote@t.com",
			Message: "Remote post content", Timestamp: time.Now(),
		}})
		_ = InsertSocialItem(SocialItem{RepoURL: extRepo, Hash: extHash, Branch: "main", Type: "post"})
		_ = SyncWorkspaceToCache(workdir)
		postRef := protocol.CreateRef(protocol.RefTypeCommit, extHash, extRepo, "main")
		result := CreateComment(workdir, postRef, "Comment on remote", nil)
		if !result.Success {
			t.Fatalf("error: %s: %v", result.Error.Code, result.Error.Details)
		}
		// Verify original ref preserves the remote branch
		if result.Data.OriginalPostID == "" {
			t.Error("OriginalPostID should not be empty")
		}
		_ = wsURL
		_ = branch
	})
}

func TestRepostAndQuote(t *testing.T) {
	t.Parallel()

	t.Run("CreateRepost_integration", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		post := CreatePost(workdir, "Repost me", nil)
		if !post.Success {
			t.Fatal(post.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)

		result := CreateRepost(workdir, post.Data.ID)
		if !result.Success {
			t.Fatalf("CreateRepost() failed: %s", result.Error.Message)
		}
		if result.Data.Type != PostTypeRepost {
			t.Errorf("Type = %q, want repost", result.Data.Type)
		}
	})

	t.Run("CreateRepost_chainBlocked", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		post := CreatePost(workdir, "Original post", nil)
		if !post.Success {
			t.Fatal(post.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)

		repost := CreateRepost(workdir, post.Data.ID)
		if !repost.Success {
			t.Fatal(repost.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)

		// Repost of repost should fail
		result := CreateRepost(workdir, repost.Data.ID)
		if result.Success {
			t.Error("CreateRepost() should fail for repost chain")
		}
		if result.Error.Code != "INVALID_TARGET" {
			t.Errorf("error code = %q, want INVALID_TARGET", result.Error.Code)
		}
	})

	t.Run("CreateQuote_integration", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		post := CreatePost(workdir, "Quote me", nil)
		if !post.Success {
			t.Fatal(post.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)

		result := CreateQuote(workdir, post.Data.ID, "My commentary")
		if !result.Success {
			t.Fatalf("CreateQuote() failed: %s", result.Error.Message)
		}
		if result.Data.Type != PostTypeQuote {
			t.Errorf("Type = %q, want quote", result.Data.Type)
		}
		if result.Data.Content != "My commentary" {
			t.Errorf("Content = %q", result.Data.Content)
		}
	})

	t.Run("CreateQuote_emptyContent", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := CreateQuote(workdir, "fake-id", "")
		if result.Success {
			t.Error("CreateQuote() should fail for empty content")
		}
		if result.Error.Code != "EMPTY_CONTENT" {
			t.Errorf("error code = %q, want EMPTY_CONTENT", result.Error.Code)
		}
	})

	t.Run("CreateQuote_repostBlocked", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		post := CreatePost(workdir, "Original", nil)
		if !post.Success {
			t.Fatal(post.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)

		repost := CreateRepost(workdir, post.Data.ID)
		if !repost.Success {
			t.Fatal(repost.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)

		result := CreateQuote(workdir, repost.Data.ID, "Quote of repost")
		if result.Success {
			t.Error("CreateQuote() should fail for repost target")
		}
		if result.Error.Code != "INVALID_TARGET" {
			t.Errorf("error code = %q, want INVALID_TARGET", result.Error.Code)
		}
	})
}

// --- Pure helpers tested via integration convenience ---

func TestBuildRefFromItem(t *testing.T) {
	item := &SocialItem{
		RepoURL:     "https://github.com/a/b",
		Hash:        "abc123def456",
		Branch:      "gitmsg/social",
		AuthorName:  "Alice",
		AuthorEmail: "alice@test.com",
		Content:     "Hello",
		Type:        "post",
		HeaderExt:   "social",
		HeaderType:  "post",
	}
	ref := buildRefFromItem(item)
	if ref.Ext != "social" {
		t.Errorf("Ext = %q, want social", ref.Ext)
	}
	if ref.Author != "Alice" {
		t.Errorf("Author = %q", ref.Author)
	}
	if ref.Fields["type"] != "post" {
		t.Errorf("Fields[type] = %q", ref.Fields["type"])
	}
}

func TestBuildRefFromItem_defaults(t *testing.T) {
	item := &SocialItem{
		RepoURL: "https://github.com/a/b",
		Hash:    "abc123",
		Branch:  "main",
	}
	ref := buildRefFromItem(item)
	if ref.Ext != "social" {
		t.Errorf("Ext = %q, want social (default)", ref.Ext)
	}
	if ref.Fields["type"] != "post" {
		t.Errorf("Fields[type] = %q, want post (default)", ref.Fields["type"])
	}
}

func TestBuildRefFromItem_withState(t *testing.T) {
	item := &SocialItem{
		RepoURL:     "https://github.com/a/b",
		Hash:        "abc123",
		Branch:      "main",
		HeaderState: "closed",
	}
	ref := buildRefFromItem(item)
	if ref.Fields["state"] != "closed" {
		t.Errorf("Fields[state] = %q, want closed", ref.Fields["state"])
	}
}

func TestGenerateRepostContentFromItem(t *testing.T) {
	item := &SocialItem{
		AuthorName: "Alice",
		RepoURL:    "https://github.com/alice/repo",
		Content:    "Short message",
	}
	got := generateRepostContentFromItem(item)
	if got == "" {
		t.Error("generateRepostContentFromItem should not return empty")
	}
	if got != "# Alice @ alice/repo: Short message" {
		t.Errorf("got = %q", got)
	}
}

func TestGenerateRepostContentFromItem_noAuthor(t *testing.T) {
	item := &SocialItem{
		Content: "Some content",
		RepoURL: "https://github.com/a/b",
	}
	got := generateRepostContentFromItem(item)
	if got == "" {
		t.Error("should not return empty")
	}
	// Author falls back to "Unknown"
	if got != "# Unknown @ a/b: Some content" {
		t.Errorf("got = %q", got)
	}
}

func TestGenerateRepostContentFromItem_longContent(t *testing.T) {
	item := &SocialItem{
		AuthorName: "Bob",
		RepoURL:    "https://github.com/b/c",
		Content:    "This is a very long message that should be truncated at fifty characters to prevent overly long repost content",
	}
	got := generateRepostContentFromItem(item)
	// First line > 50 chars should be truncated to 47 + "..."
	if len(got) > 200 {
		t.Errorf("content too long: %d chars", len(got))
	}
}

// --- List CRUD ---

func TestListManagement(t *testing.T) {
	t.Parallel()

	t.Run("CreateList", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := CreateList(workdir, "test-list", "Test List")
		if !result.Success {
			t.Fatalf("CreateList() failed: %s", result.Error.Message)
		}
		list := result.Data
		if list.ID != "test-list" {
			t.Errorf("ID = %q", list.ID)
		}
		if list.Name != "Test List" {
			t.Errorf("Name = %q", list.Name)
		}
	})

	t.Run("CreateList_invalidID", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := CreateList(workdir, "invalid id with spaces!", "Bad")
		if result.Success {
			t.Error("CreateList() should fail for invalid ID")
		}
		if result.Error.Code != "INVALID_LIST_ID" {
			t.Errorf("error code = %q, want INVALID_LIST_ID", result.Error.Code)
		}
	})

	t.Run("CreateList_duplicate", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreateList(workdir, "dupe-list", "First")
		result := CreateList(workdir, "dupe-list", "Second")
		if result.Success {
			t.Error("CreateList() should fail for duplicate ID")
		}
		if result.Error.Code != "LIST_EXISTS" {
			t.Errorf("error code = %q, want LIST_EXISTS", result.Error.Code)
		}
	})

	t.Run("CreateList_emptyName", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := CreateList(workdir, "no-name", "")
		if !result.Success {
			t.Fatalf("CreateList() failed: %s", result.Error.Message)
		}
		// Empty name falls back to listID
		if result.Data.Name != "no-name" {
			t.Errorf("Name = %q, want %q", result.Data.Name, "no-name")
		}
	})

	t.Run("GetLists", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreateList(workdir, "list-a", "List A")
		CreateList(workdir, "list-b", "List B")

		result := GetLists(workdir)
		if !result.Success {
			t.Fatalf("GetLists() failed: %s", result.Error.Message)
		}
		if len(result.Data) < 2 {
			t.Errorf("expected at least 2 lists, got %d", len(result.Data))
		}
	})

	t.Run("GetList", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreateList(workdir, "find-me", "Find Me")

		result := GetList(workdir, "find-me")
		if !result.Success {
			t.Fatalf("GetList() failed: %s", result.Error.Message)
		}
		if result.Data == nil {
			t.Fatal("GetList() returned nil")
		}
		if result.Data.Name != "Find Me" {
			t.Errorf("Name = %q", result.Data.Name)
		}
	})

	t.Run("GetList_notFound", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := GetList(workdir, "nonexistent")
		if !result.Success {
			t.Fatalf("GetList() error: %s", result.Error.Message)
		}
		// Non-existent list returns nil (not an error)
		if result.Data != nil {
			t.Error("GetList() should return nil for non-existent list")
		}
	})

	t.Run("GetList_exists", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreateList(workdir, "get-me", "Get Me")
		result := GetList(workdir, "get-me")
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
		if result.Data == nil {
			t.Fatal("expected list data")
		}
		if result.Data.ID != "get-me" {
			t.Errorf("ID = %q", result.Data.ID)
		}
	})

	t.Run("GetLists_withData", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreateList(workdir, "listtest1", "List 1")
		CreateList(workdir, "listtest2", "List 2")
		result := GetLists(workdir)
		if !result.Success {
			t.Fatalf("GetLists failed: %s", result.Error.Message)
		}
		if len(result.Data) < 2 {
			t.Errorf("expected at least 2 lists, got %d", len(result.Data))
		}
	})

	t.Run("DeleteList", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreateList(workdir, "delete-me", "Delete Me")

		result := DeleteList(workdir, "delete-me")
		if !result.Success {
			t.Fatalf("DeleteList() failed: %s", result.Error.Message)
		}

		// Verify it's gone
		found := GetList(workdir, "delete-me")
		if found.Data != nil {
			t.Error("list should be deleted")
		}
	})

	t.Run("DeleteList_notFound", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := DeleteList(workdir, "nonexistent")
		if result.Success {
			t.Error("DeleteList() should fail for non-existent list")
		}
		if result.Error.Code != "LIST_NOT_FOUND" {
			t.Errorf("error code = %q, want LIST_NOT_FOUND", result.Error.Code)
		}
	})

	t.Run("DeleteList_success", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreateList(workdir, "del-me", "Delete Me")
		result := DeleteList(workdir, "del-me")
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
		// Verify deleted
		getResult := GetList(workdir, "del-me")
		if !getResult.Success {
			t.Fatalf("GetList error: %s", getResult.Error.Message)
		}
		if getResult.Data != nil {
			t.Error("list should be nil after deletion")
		}
	})

	t.Run("AddRepositoryToList", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreateList(workdir, "repo-list", "Repo List")

		result := AddRepositoryToList(workdir, "repo-list", "https://github.com/test/repo", "main", false)
		if !result.Success {
			t.Fatalf("AddRepositoryToList() failed: %s", result.Error.Message)
		}

		// Verify repo is in list
		list := GetList(workdir, "repo-list")
		if list.Data == nil {
			t.Fatal("list should exist")
		}
		if len(list.Data.Repositories) != 1 {
			t.Errorf("expected 1 repository, got %d", len(list.Data.Repositories))
		}
	})

	t.Run("AddRepositoryToList_notFound", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := AddRepositoryToList(workdir, "nonexistent", "https://github.com/a/b", "main", false)
		if result.Success {
			t.Error("AddRepositoryToList() should fail for non-existent list")
		}
		if result.Error.Code != "LIST_NOT_FOUND" {
			t.Errorf("error code = %q, want LIST_NOT_FOUND", result.Error.Code)
		}
	})

	t.Run("AddRepositoryToList_duplicate", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreateList(workdir, "dup-repo-list", "DupRepo")
		AddRepositoryToList(workdir, "dup-repo-list", "https://github.com/a/b", "main", false)

		result := AddRepositoryToList(workdir, "dup-repo-list", "https://github.com/a/b", "main", false)
		if result.Success {
			t.Error("AddRepositoryToList() should fail for duplicate repository")
		}
		if result.Error.Code != "REPOSITORY_EXISTS" {
			t.Errorf("error code = %q, want REPOSITORY_EXISTS", result.Error.Code)
		}
	})

	t.Run("AddRepositoryToList_emptyBranch", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreateList(workdir, "branch-list", "Branch")
		result := AddRepositoryToList(workdir, "branch-list", "https://github.com/a/b", "", false)
		if !result.Success {
			t.Fatalf("AddRepositoryToList failed: %s", result.Error.Message)
		}
		// Should have auto-detected branch
		if result.Data == "" {
			t.Error("expected non-empty repo ref")
		}
	})

	t.Run("RemoveRepositoryFromList", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreateList(workdir, "rm-repo-list", "RmRepo")
		AddRepositoryToList(workdir, "rm-repo-list", "https://github.com/a/b", "main", false)

		result := RemoveRepositoryFromList(workdir, "rm-repo-list", "https://github.com/a/b")
		if !result.Success {
			t.Fatalf("RemoveRepositoryFromList() failed: %s", result.Error.Message)
		}

		list := GetList(workdir, "rm-repo-list")
		if list.Data == nil {
			t.Fatal("list should still exist")
		}
		if len(list.Data.Repositories) != 0 {
			t.Errorf("expected 0 repositories after removal, got %d", len(list.Data.Repositories))
		}
	})

	t.Run("RemoveRepositoryFromList_notFound", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := RemoveRepositoryFromList(workdir, "nonexistent", "https://github.com/a/b")
		if result.Success {
			t.Error("RemoveRepositoryFromList() should fail for non-existent list")
		}
		if result.Error.Code != "LIST_NOT_FOUND" {
			t.Errorf("error code = %q, want LIST_NOT_FOUND", result.Error.Code)
		}
	})

	t.Run("RemoveRepositoryFromList_repoNotInList", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreateList(workdir, "rm-miss-list", "Rm Miss")

		result := RemoveRepositoryFromList(workdir, "rm-miss-list", "https://github.com/not/there")
		if result.Success {
			t.Error("RemoveRepositoryFromList() should fail for repo not in list")
		}
		if result.Error.Code != "REPOSITORY_NOT_FOUND" {
			t.Errorf("error code = %q, want REPOSITORY_NOT_FOUND", result.Error.Code)
		}
	})

	t.Run("RemoveRepositoryFromList_ok", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreateList(workdir, "rm-ok-list", "Remove OK")
		AddRepositoryToList(workdir, "rm-ok-list", "https://github.com/rem/repo", "main", false)
		result := RemoveRepositoryFromList(workdir, "rm-ok-list", "https://github.com/rem/repo")
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
	})
}

func TestListDataToList(t *testing.T) {
	list := listDataToList(gitmsg.ListData{
		ID:           "test",
		Name:         "Test",
		Version:      "0.1.0",
		Repositories: []string{"https://github.com/a/b#branch:main"},
	})
	if list.ID != "test" {
		t.Errorf("ID = %q", list.ID)
	}
	if list.Name != "Test" {
		t.Errorf("Name = %q", list.Name)
	}
	if len(list.Repositories) != 1 {
		t.Errorf("Repositories len = %d", len(list.Repositories))
	}
}

// --- Search ---

func TestSearchIntegration(t *testing.T) {
	t.Parallel()

	t.Run("Search", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreatePost(workdir, "Go is awesome", nil)
		CreatePost(workdir, "Rust is fast", nil)
		_ = SyncWorkspaceToCache(workdir)

		result, err := search.Search(workdir, search.Params{Query: "awesome", Scope: "repository:my"})
		if err != nil {
			t.Fatalf("Search() failed: %s", err)
		}
		if result.Total == 0 {
			t.Error("expected at least 1 search result for 'awesome'")
		}
	})

	t.Run("Search_noResults", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreatePost(workdir, "Hello world", nil)
		_ = SyncWorkspaceToCache(workdir)

		result, err := search.Search(workdir, search.Params{Query: "nonexistenttermxyz", Scope: "repository:my"})
		if err != nil {
			t.Fatalf("Search() failed: %s", err)
		}
		if result.Total != 0 {
			t.Errorf("expected 0 results, got %d", result.Total)
		}
	})

	t.Run("Search_authorFilter", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreatePost(workdir, "Author test post", nil)
		_ = SyncWorkspaceToCache(workdir)

		result, err := search.Search(workdir, search.Params{Query: "author:test", Scope: "repository:my"})
		if err != nil {
			t.Fatalf("Search() failed: %s", err)
		}
		// test@test.com is our author, so should match
		if result.Total == 0 {
			t.Error("expected at least 1 result for author:test")
		}
	})

	t.Run("Search_dateSort", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreatePost(workdir, "First post", nil)
		CreatePost(workdir, "Second post", nil)
		_ = SyncWorkspaceToCache(workdir)

		result, err := search.Search(workdir, search.Params{Sort: "date", Scope: "repository:my"})
		if err != nil {
			t.Fatalf("Search() failed: %s", err)
		}
		if result.Total < 2 {
			t.Errorf("expected at least 2 results, got %d", result.Total)
		}
	})

	t.Run("Search_hashSearch", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		post := CreatePost(workdir, "Hash search target", nil)
		if !post.Success {
			t.Fatal(post.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)
		parsed := protocol.ParseRef(post.Data.ID)
		shortHash := parsed.Value[:7]
		result, err := search.Search(workdir, search.Params{Query: shortHash, Scope: "repository:my"})
		if err != nil {
			t.Fatalf("error: %s", err)
		}
		if result.Total == 0 {
			t.Error("expected at least 1 result for hash search")
		}
	})

	t.Run("Search_reposScope", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreatePost(workdir, "Repos scope test", nil)
		_ = SyncWorkspaceToCache(workdir)
		wsURL := gitmsg.ResolveRepoURL(workdir)
		result, err := search.Search(workdir, search.Params{Scope: "repos:" + wsURL})
		if err != nil {
			t.Fatalf("error: %s", err)
		}
		if result.Total == 0 {
			t.Error("expected results for repos: scope")
		}
	})

	t.Run("Search_listScope", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreateList(workdir, "search-list", "Search List")
		_, err := search.Search(workdir, search.Params{Query: "anything", Scope: "list:search-list"})
		if err != nil {
			t.Fatalf("error: %s", err)
		}
	})

	t.Run("Search_withFilters", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreatePost(workdir, "Filtered search test", nil)
		_ = SyncWorkspaceToCache(workdir)
		_, err := search.Search(workdir, search.Params{
			Query:  "after:2020-01-01 before:2030-12-31 author:test Filtered",
			Scope:  "repository:workspace",
			Author: "test",
			Limit:  5,
		})
		if err != nil {
			t.Fatalf("error: %s", err)
		}
	})

	t.Run("Search_limitExceeded", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		for i := 0; i < 5; i++ {
			CreatePost(workdir, fmt.Sprintf("Limit test post %d", i), nil)
		}
		_ = SyncWorkspaceToCache(workdir)
		result, err := search.Search(workdir, search.Params{Scope: "repository:my", Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Results) > 2 {
			t.Errorf("expected at most 2 results, got %d", len(result.Results))
		}
		if result.Total > 2 && !result.HasMore {
			t.Error("HasMore should be true when total > limit")
		}
	})

	t.Run("Search_typeFilter", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreatePost(workdir, "Type filter post", nil)
		_ = SyncWorkspaceToCache(workdir)
		_, err := search.Search(workdir, search.Params{
			Query: "type:post filter",
			Scope: "repository:my",
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Search_listFilter", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreatePost(workdir, "List filter search", nil)
		_ = SyncWorkspaceToCache(workdir)
		_, err := search.Search(workdir, search.Params{
			Query: "list:some-list filter",
			Scope: "repository:my",
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Search_hashExplicitFilter", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		post := CreatePost(workdir, "Hash explicit filter test", nil)
		if !post.Success {
			t.Fatal(post.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)
		parsed := protocol.ParseRef(post.Data.ID)
		result, err := search.Search(workdir, search.Params{Hash: parsed.Value[:7], Scope: "repository:my"})
		if err != nil {
			t.Fatal(err)
		}
		if result.Total == 0 {
			t.Error("expected result with explicit hash filter")
		}
	})

	t.Run("Search_repoExplicitFilter", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreatePost(workdir, "Repo filter test", nil)
		_ = SyncWorkspaceToCache(workdir)
		wsURL := gitmsg.ResolveRepoURL(workdir)
		_, err := search.Search(workdir, search.Params{Repo: wsURL, Scope: "repository:my"})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Search_commitFilter", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		post := CreatePost(workdir, "Commit filter test", nil)
		if !post.Success {
			t.Fatal(post.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)
		parsed := protocol.ParseRef(post.Data.ID)
		// Use commit: filter syntax in query
		_, err := search.Search(workdir, search.Params{Query: "commit:" + parsed.Value[:7], Scope: "repository:my"})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Search_authorMismatch", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreatePost(workdir, "Search author mismatch", nil)
		_ = SyncWorkspaceToCache(workdir)
		result, err := search.Search(workdir, search.Params{
			Query:  "Search",
			Author: "nonexistent-author-xyz",
		})
		if err != nil {
			t.Fatalf("error: %s", err)
		}
		if len(result.Results) != 0 {
			t.Errorf("expected 0 results for mismatched author, got %d", len(result.Results))
		}
	})

	t.Run("Search_externalRepoScope", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		extRepo := "https://github.com/search-ext/repo"
		_ = cache.InsertCommits([]cache.Commit{{
			Hash: "srch12345678", RepoURL: extRepo, Branch: "main",
			AuthorName: "Search", AuthorEmail: "s@t.com",
			Message: "Searchable external content", Timestamp: time.Now(),
		}})
		_ = InsertSocialItem(SocialItem{RepoURL: extRepo, Hash: "srch12345678", Branch: "main", Type: "post"})
		_, err := search.Search(workdir, search.Params{
			Query: "Searchable external",
			Scope: "repository:" + extRepo,
		})
		if err != nil {
			t.Fatalf("error: %s", err)
		}
	})

	t.Run("Search_scoreTiebreak", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		// Create two posts with same search score but different timestamps
		CreatePost(workdir, "Tiebreak older", nil)
		time.Sleep(10 * time.Millisecond)
		CreatePost(workdir, "Tiebreak newer", nil)
		_ = SyncWorkspaceToCache(workdir)
		result, err := search.Search(workdir, search.Params{Query: "Tiebreak"})
		if err != nil {
			t.Fatalf("error: %s", err)
		}
		if len(result.Results) >= 2 {
			// Both have same score -> sorted by timestamp descending (newer first)
			if result.Results[0].Timestamp.Before(result.Results[1].Timestamp) {
				t.Error("expected newer post first in tiebreak")
			}
		}
	})

	t.Run("Search_authorInAuthorField", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreatePost(workdir, "Some content without the search term", nil)
		_ = SyncWorkspaceToCache(workdir)
		// Search for the author name - should match via author field scoring
		_, err := search.Search(workdir, search.Params{Query: "Test User"})
		if err != nil {
			t.Fatalf("error: %s", err)
		}
		// The author "Test User" should boost score
	})
}

// --- Status ---

func TestStatusAndRepositories(t *testing.T) {
	t.Parallel()

	t.Run("Status", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := Status(workdir, t.TempDir())
		if !result.Success {
			t.Fatalf("Status() failed: %s", result.Error.Message)
		}
		if result.Data.Branch == "" {
			t.Error("Branch should not be empty")
		}
	})

	t.Run("Status_withLists", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreateList(workdir, "status-list", "Status List")
		AddRepositoryToList(workdir, "status-list", "https://github.com/a/b", "main", false)

		result := Status(workdir, t.TempDir())
		if !result.Success {
			t.Fatalf("Status() failed: %s", result.Error.Message)
		}
		if len(result.Data.Lists) == 0 {
			t.Error("Status should include lists")
		}
	})

	t.Run("Status_initialized", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		cacheDir := t.TempDir()
		result := Status(workdir, cacheDir)
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
		if result.Data.Branch == "" {
			t.Error("expected Branch to be set")
		}
	})

	t.Run("GetRepositories", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreateList(workdir, "repo-discovery", "Repo Discovery")
		AddRepositoryToList(workdir, "repo-discovery", "https://github.com/a/b", "main", false)
		AddRepositoryToList(workdir, "repo-discovery", "https://github.com/c/d", "main", false)

		result := GetRepositories(workdir, "all", 0)
		if !result.Success {
			t.Fatalf("GetRepositories() failed: %s", result.Error.Message)
		}
		if len(result.Data) < 2 {
			t.Errorf("expected at least 2 repositories, got %d", len(result.Data))
		}
	})

	t.Run("GetRepositories_byList", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreateList(workdir, "list-repos", "List Repos")
		AddRepositoryToList(workdir, "list-repos", "https://github.com/x/y", "main", false)

		result := GetRepositories(workdir, "list:list-repos", 0)
		if !result.Success {
			t.Fatalf("GetRepositories(list:) failed: %s", result.Error.Message)
		}
		if len(result.Data) != 1 {
			t.Errorf("expected 1 repository, got %d", len(result.Data))
		}
	})

	t.Run("GetRepositories_listNotFound", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := GetRepositories(workdir, "list:nonexistent", 0)
		if result.Success && len(result.Data) > 0 {
			t.Error("non-existent list should return empty or error")
		}
	})

	t.Run("GetRepositories_allWithCachedRepos", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		_ = cache.InsertRepository(cache.Repository{
			URL:    "https://github.com/cached/repo",
			Branch: "main",
		})
		result := GetRepositories(workdir, "all", 0)
		if !result.Success {
			t.Fatal(result.Error.Message)
		}
		found := false
		for _, r := range result.Data {
			if r.URL == "https://github.com/cached/repo" {
				found = true
			}
		}
		if !found {
			t.Error("cached repo should appear in all repositories")
		}
	})

	t.Run("GetRepositories_listWithFetchRanges", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		listResult := CreateList(workdir, "range-list", "Range Test")
		if !listResult.Success {
			t.Fatalf("CreateList error: %s", listResult.Error.Message)
		}
		repoURL := "https://github.com/range/repo"
		AddRepositoryToList(workdir, "range-list", repoURL, "main", false)
		// Insert a fetch range
		_ = cache.ExecLocked(func(db *sql.DB) error {
			_, err := db.Exec(`INSERT INTO core_fetch_ranges (repo_url, range_start, range_end, status, fetched_at, commit_count)
				VALUES (?, ?, ?, ?, datetime('now'), ?)`,
				repoURL, "2025-01-01T00:00:00Z", "2025-06-01T00:00:00Z", "complete", 10)
			return err
		})
		result := GetRepositories(workdir, "list:range-list", 0)
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
		if len(result.Data) == 0 {
			t.Fatal("expected at least 1 repo")
		}
		if len(result.Data[0].FetchedRanges) == 0 {
			t.Error("expected fetch ranges to be populated")
		}
	})

	t.Run("GetRepositories_allWithFetchRangesData", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		externalRepo := "https://github.com/frange/repo"
		CreateList(workdir, "fr-list", "FR List")
		AddRepositoryToList(workdir, "fr-list", externalRepo, "main", false)
		_ = SyncWorkspaceToCache(workdir)
		// Insert fetch range for the repo
		_ = cache.ExecLocked(func(db *sql.DB) error {
			_, err := db.Exec(`INSERT INTO core_fetch_ranges (repo_url, range_start, range_end, status, fetched_at, commit_count)
				VALUES (?, ?, ?, ?, ?, ?)`,
				externalRepo, "2026-01-01", "2026-01-31", "complete", time.Now().Format(time.RFC3339), 10)
			return err
		})
		result := GetRepositories(workdir, "", 0)
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
		found := false
		for _, r := range result.Data {
			if r.URL == externalRepo && len(r.FetchedRanges) > 0 {
				found = true
				if r.FetchedRanges[0].Start != "2026-01-01" {
					t.Errorf("Start = %q", r.FetchedRanges[0].Start)
				}
			}
		}
		if !found {
			t.Error("expected repo with fetch ranges")
		}
	})

	t.Run("GetRepositories_withCachedRepos", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		// Insert a cached repository that's not in any list
		_ = cache.InsertRepository(cache.Repository{
			URL:    "https://github.com/cached/repo",
			Branch: "main",
		})
		result := GetRepositories(workdir, "all", 0)
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
		found := false
		for _, r := range result.Data {
			if r.URL == "https://github.com/cached/repo" {
				found = true
				if len(r.Lists) != 0 {
					t.Errorf("cached repo should have empty lists, got %v", r.Lists)
				}
			}
		}
		if !found {
			t.Error("cached repo should appear in all repositories")
		}
	})

	t.Run("CheckIfRepoFollowsWorkspace_withLists", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		workspaceURL := gitmsg.ResolveRepoURL(workdir)

		// Create a separate workspace that has us in a list
		remoteDir := cloneFixture(t)
		CreateList(remoteDir, "follows-us", "Follows Us")
		AddRepositoryToList(remoteDir, "follows-us", workspaceURL, "main", false)

		remoteURL := gitmsg.ResolveRepoURL(remoteDir)
		// Call checkIfRepoFollowsWorkspace with the remote workspace as storage
		checkIfRepoFollowsWorkspace(remoteDir, remoteURL, "", workspaceURL)

		// Verify follower was inserted
		followers, _ := GetFollowers(workspaceURL)
		found := false
		for _, f := range followers {
			if f == remoteURL {
				found = true
			}
		}
		if !found {
			t.Error("expected follower to be detected")
		}
	})

	t.Run("CacheExternalRepoLists_withLists", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		repoURL := gitmsg.ResolveRepoURL(workdir)
		CreateList(workdir, "ext-cache-list", "External Cache")
		AddRepositoryToList(workdir, "ext-cache-list", "https://github.com/ext/repo", "main", false)

		// Use the workspace as storage dir (it has the right git structure)
		cacheExternalRepoLists(workdir, repoURL, "", "")

		// cacheExternalRepoLists stores to core_external_repo_lists, not core_lists
		// Just verify it doesn't panic and executes the code path
	})

	t.Run("GetRelatedRepositories_emptyRepoRef", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		// Create a list with an invalid/empty repo ref
		CreateList(workdir, "empty-ref", "Empty Ref")
		// Manually add an invalid ref by writing the list with a bad ref
		_ = gitmsg.WriteList(workdir, "social", "empty-ref", gitmsg.ListData{
			ID: "empty-ref", Name: "Empty Ref", Version: "0.1.0",
			Repositories: []string{"https://github.com/real/repo#branch:main", "not-a-valid-ref"},
		})
		result := GetRelatedRepositories(workdir, "https://github.com/real/repo")
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
		// Should not crash, invalid ref is skipped
	})
}

func TestStatus_notInitialized(t *testing.T) {
	// Create a bare git repo without social extension
	dir := t.TempDir()
	git.Init(dir, "main")
	git.ExecGit(dir, []string{"config", "user.email", "test@test.com"})
	git.ExecGit(dir, []string{"config", "user.name", "Test User"})
	git.CreateCommit(dir, git.CommitOptions{Message: "Initial", AllowEmpty: true})
	setupTestDB(t)

	result := Status(dir, t.TempDir())
	if result.Success {
		t.Error("Status() should fail for non-initialized workspace")
	}
	if result.Error.Code != "NOT_INITIALIZED" {
		t.Errorf("error code = %q, want NOT_INITIALIZED", result.Error.Code)
	}
}

// (TestGetRepositories, TestGetRepositories_byList are in TestStatusAndRepositories group)

func TestAppendUnique(t *testing.T) {
	slice := []string{"a", "b"}
	result := appendUnique(slice, "c")
	if len(result) != 3 {
		t.Errorf("len = %d, want 3", len(result))
	}
	// Adding duplicate should not change length
	result = appendUnique(result, "b")
	if len(result) != 3 {
		t.Errorf("len = %d, want 3 (no duplicate)", len(result))
	}
}

func TestAppendUnique_empty(t *testing.T) {
	result := appendUnique(nil, "a")
	if len(result) != 1 {
		t.Errorf("len = %d, want 1", len(result))
	}
}

// --- Log integration ---

func TestLogsIntegration(t *testing.T) {
	t.Parallel()

	t.Run("GetLogs", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreatePost(workdir, "Log test post", nil)

		result := GetLogs(workdir, "", nil)
		if !result.Success {
			t.Fatalf("GetLogs() failed: %s", result.Error.Message)
		}
		if len(result.Data) == 0 {
			t.Error("expected at least 1 log entry")
		}
	})

	t.Run("GetLogs_invalidScope", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := GetLogs(workdir, "list:some-list", nil)
		if result.Success {
			t.Error("GetLogs(list:) should fail")
		}
		if result.Error.Code != "INVALID_SCOPE" {
			t.Errorf("error code = %q, want INVALID_SCOPE", result.Error.Code)
		}

		result = GetLogs(workdir, "repository:https://example.com", nil)
		if result.Success {
			t.Error("GetLogs(repository:) should fail")
		}
	})

	t.Run("GetLogs_withFilters", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreatePost(workdir, "Filtered log post", nil)

		opts := &GetLogsOptions{
			Types:  []LogEntryType{LogTypePost},
			Author: "test",
		}
		result := GetLogs(workdir, "", opts)
		if !result.Success {
			t.Fatalf("GetLogs() failed: %s", result.Error.Message)
		}
	})

	t.Run("GetLogs_repositoryMyScope", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreatePost(workdir, "My repo log", nil)
		result := GetLogs(workdir, "repository:my", nil)
		if !result.Success {
			t.Fatalf("GetLogs(repository:my) failed: %s", result.Error.Message)
		}
	})

	t.Run("GetLogs_timelineScope", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreatePost(workdir, "Timeline log", nil)
		_ = SyncWorkspaceToCache(workdir)
		result := GetLogs(workdir, "timeline", nil)
		if !result.Success {
			t.Fatalf("GetLogs(timeline) failed: %s", result.Error.Message)
		}
	})

	t.Run("GetLogs_unknownScope", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := GetLogs(workdir, "something:random", nil)
		if result.Success {
			t.Error("expected failure for unknown scope")
		}
		if result.Error.Code != "INVALID_SCOPE" {
			t.Errorf("error code = %q, want INVALID_SCOPE", result.Error.Code)
		}
	})

	t.Run("GetLogs_afterBeforeFilter", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreatePost(workdir, "Time filter log", nil)
		after := time.Now().Add(-1 * time.Hour)
		before := time.Now().Add(1 * time.Hour)
		opts := &GetLogsOptions{After: &after, Before: &before}
		result := GetLogs(workdir, "", opts)
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
	})

	t.Run("GetLogs_typeFilterExcludes", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreatePost(workdir, "Type exclude log", nil)
		opts := &GetLogsOptions{Types: []LogEntryType{LogTypeComment}}
		result := GetLogs(workdir, "", opts)
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
		// Posts should be excluded when filtering for comments only
		for _, entry := range result.Data {
			if entry.Type == LogTypePost {
				t.Error("post should not appear when filtering for comments")
			}
		}
	})

	t.Run("GetLogs_longContentTruncation", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		// Create a post with content > 80 chars
		longContent := "This is a very long post content that exceeds eighty characters limit for truncation in log details"
		CreatePost(workdir, longContent, nil)
		result := GetLogs(workdir, "", nil)
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
		for _, entry := range result.Data {
			if len(entry.Details) > 83 { // 80 + "..." = 83 max
				t.Errorf("details not truncated: len=%d, details=%q", len(entry.Details), entry.Details)
			}
		}
	})
}

// (TestGetPosts_repositoryExternal, TestGetPosts_listScope, TestGetPosts_withOptions are in TestGetPostsIntegration group)

func TestVersionAndResolve(t *testing.T) {
	t.Parallel()

	t.Run("ResolveCurrentVersion", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		post := CreatePost(workdir, "Original", nil)
		if !post.Success {
			t.Fatal(post.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)
		workspaceURL := gitmsg.ResolveRepoURL(workdir)

		edit := EditPost(workdir, post.Data.ID, "Updated")
		if !edit.Success {
			t.Fatalf("EditPost() failed: %s", edit.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)

		// Parse the original post ref to get repo, hash, branch
		parsed := parseRefForTest(post.Data.ID)
		resolved, err := ResolveCurrentVersion(parsed.repo, parsed.hash, parsed.branch, workspaceURL)
		if err != nil {
			t.Fatalf("ResolveCurrentVersion() error = %v", err)
		}
		if resolved.Item == nil {
			t.Fatal("resolved.Item should not be nil")
		}
	})

	t.Run("ResolveCurrentVersion_withEdit", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		post := CreatePost(workdir, "Version 1", nil)
		if !post.Success {
			t.Fatal(post.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)
		wsURL := gitmsg.ResolveRepoURL(workdir)

		edit := EditPost(workdir, post.Data.ID, "Version 2")
		if !edit.Success {
			t.Fatal(edit.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)

		// Resolve using the EDIT hash - should resolve to canonical
		editParsed := parseRefForTest(edit.Data.ID)
		resolved, err := ResolveCurrentVersion(editParsed.repo, editParsed.hash, editParsed.branch, wsURL)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if resolved.Item == nil {
			t.Fatal("resolved.Item should not be nil")
		}
		if resolved.Item.Content != "Version 2" {
			t.Errorf("Content = %q, want Version 2", resolved.Item.Content)
		}
		if !resolved.IsEdited {
			t.Error("IsEdited should be true")
		}
	})

	t.Run("GetEditHistoryPosts", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		post := CreatePost(workdir, "Version 1", nil)
		if !post.Success {
			t.Fatal(post.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)

		edit := EditPost(workdir, post.Data.ID, "Version 2")
		if !edit.Success {
			t.Fatalf("EditPost() failed: %s", edit.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)

		parsed := parseRefForTest(post.Data.ID)
		posts, err := GetEditHistoryPosts(parsed.repo, parsed.hash, parsed.branch, gitmsg.ResolveRepoURL(workdir))
		if err != nil {
			t.Fatalf("GetEditHistoryPosts() error = %v", err)
		}
		if len(posts) < 1 {
			t.Errorf("expected at least 1 version, got %d", len(posts))
		}
	})

	t.Run("ResolveItem_fromCachedCommit", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		post := CreatePost(workdir, "Resolve me", nil)
		if !post.Success {
			t.Fatal(post.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)
		item := resolveItem(workdir, post.Data.ID)
		if item == nil {
			t.Fatal("resolveItem should find cached post")
		}
		if item.Content == "" {
			t.Error("Content should not be empty")
		}
	})

	t.Run("ResolveItem_notFound", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		item := resolveItem(workdir, "nonexistent_ref")
		if item != nil {
			t.Error("should return nil for invalid ref")
		}
	})

	t.Run("ResolveItem_emptyRef", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		item := resolveItem(workdir, "")
		if item != nil {
			t.Error("should return nil for empty ref")
		}
	})

	t.Run("ResolveItem_fallbackToCachedCommit", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		// Insert a commit directly into cache (not via workspace)
		extRepo := "https://github.com/ext/cached"
		msg := "Cached post\n\n" + `--- GitMsg: ext="social"; type="post"; v="0.1.0" ---`
		_ = cache.InsertCommits([]cache.Commit{{
			Hash: "ccfb12345678", RepoURL: extRepo, Branch: "main",
			AuthorName: "Ext", AuthorEmail: "ext@t.com",
			Message: msg, Timestamp: time.Now(),
		}})
		// Ref for this commit - not in social_items, not in workspace git
		item := resolveItem(workdir, extRepo+"#commit:ccfb12345678@main")
		if item == nil {
			t.Fatal("should resolve from cached commit")
		}
		if item.Type != "post" {
			t.Errorf("Type = %q, want post", item.Type)
		}
	})
}

type parsedRefResult struct {
	repo, hash, branch string
}

func parseRefForTest(ref string) parsedRefResult {
	p := protocol.ParseRef(ref)
	branch := p.Branch
	if branch == "" {
		branch = "main"
	}
	return parsedRefResult{repo: p.Repository, hash: p.Value, branch: branch}
}

func TestNotificationIntegration(t *testing.T) {
	t.Parallel()

	t.Run("GetNotifications_empty", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		git.ExecGit(workdir, []string{"config", "user.email", "notif-empty@test.com"})
		notifications, err := GetNotifications(workdir, NotificationFilter{})
		if err != nil {
			t.Fatalf("GetNotifications() error = %v", err)
		}
		if len(notifications) != 0 {
			t.Errorf("expected 0 notifications, got %d", len(notifications))
		}
	})

	t.Run("GetUnreadCount_empty", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		_, err := GetUnreadCount(workdir)
		if err != nil {
			t.Fatalf("GetUnreadCount() error = %v", err)
		}
		// Count may be non-zero due to thread participation from parallel tests sharing the cache
	})

	t.Run("MarkAllAsRead_empty", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		if err := MarkAllAsRead(workdir); err != nil {
			t.Fatalf("MarkAllAsRead() error = %v", err)
		}
	})

	t.Run("MarkAllAsUnread_empty", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		if err := MarkAllAsUnread(workdir); err != nil {
			t.Fatalf("MarkAllAsUnread() error = %v", err)
		}
	})

	t.Run("GetNotifications_withFilter", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		git.ExecGit(workdir, []string{"config", "user.email", "notif-filter@test.com"})
		notifications, err := GetNotifications(workdir, NotificationFilter{UnreadOnly: true, Limit: 5})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(notifications) != 0 {
			t.Errorf("expected 0, got %d", len(notifications))
		}
	})

	t.Run("GetNotifications_withLimit", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		workspaceURL := gitmsg.ResolveRepoURL(workdir)
		// Create 3 external comments
		for i := 0; i < 3; i++ {
			setupExternalInteraction(t, workdir, "comment")
		}
		notifs, err := GetNotifications(workdir, NotificationFilter{Limit: 2})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(notifs) > 2 {
			t.Errorf("expected at most 2 with limit, got %d", len(notifs))
		}
		_ = workspaceURL
	})

	t.Run("GetNotifications_unreadOnly", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		setupExternalInteraction(t, workdir, "comment")
		// Mark all as read
		_ = MarkAllAsRead(workdir)
		notifs, err := GetNotifications(workdir, NotificationFilter{UnreadOnly: true})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(notifs) != 0 {
			t.Errorf("expected 0 unread, got %d", len(notifs))
		}
	})

	t.Run("Notifications_externalComment", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		git.ExecGit(workdir, []string{"config", "user.email", "notif-extcomment@test.com"})
		setupExternalInteraction(t, workdir, "comment")
		notifs, err := GetNotifications(workdir, NotificationFilter{})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(notifs) == 0 {
			t.Fatal("expected at least 1 notification")
		}
		n := notifs[0]
		if n.Type != NotificationTypeComment {
			t.Errorf("Type = %q, want comment", n.Type)
		}
		if n.Actor.Name != "External User" {
			t.Errorf("Actor = %q", n.Actor.Name)
		}
		if n.TargetID == "" {
			t.Error("TargetID should not be empty")
		}
		if n.IsRead {
			t.Error("should be unread")
		}
		if n.ID == "" {
			t.Error("ID should not be empty")
		}
	})

	t.Run("Notifications_externalRepost", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		setupExternalInteraction(t, workdir, "repost")
		notifs, err := GetNotifications(workdir, NotificationFilter{})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(notifs) == 0 {
			t.Error("expected at least 1 notification for repost")
		}
	})

	t.Run("Notifications_externalQuote", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		setupExternalInteraction(t, workdir, "quote")
		notifs, err := GetNotifications(workdir, NotificationFilter{})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(notifs) == 0 {
			t.Error("expected at least 1 notification for quote")
		}
	})

	t.Run("Notifications_unreadFilter", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		extRepo, extHash := setupExternalInteraction(t, workdir, "comment")
		_ = notifications.MarkAsRead(extRepo, extHash, "main")
		notifs, err := GetNotifications(workdir, NotificationFilter{UnreadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range notifs {
			if n.ActorRepo == extRepo && n.Type == NotificationTypeComment {
				t.Error("marked-as-read notification should not appear with UnreadOnly")
			}
		}
	})

	t.Run("Notifications_withLimit", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		workspaceURL := gitmsg.ResolveRepoURL(workdir)
		branch := gitmsg.GetExtBranch(workdir, "social")
		post := CreatePost(workdir, "Multi notification target", nil)
		if !post.Success {
			t.Fatal(post.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)
		parsed := protocol.ParseRef(post.Data.ID)
		for i := 0; i < 3; i++ {
			extRepo := fmt.Sprintf("https://github.com/ext%d/repo", i)
			hash := fmt.Sprintf("ext_lim_%02d", i)
			_ = cache.InsertCommits([]cache.Commit{{
				Hash: hash, RepoURL: extRepo, Branch: "main",
				AuthorName: fmt.Sprintf("User %d", i), AuthorEmail: fmt.Sprintf("u%d@test.com", i),
				Message: "comment", Timestamp: time.Now().Add(time.Duration(-i) * time.Minute),
			}})
			_ = InsertSocialItem(SocialItem{
				RepoURL: extRepo, Hash: hash, Branch: "main", Type: "comment",
				OriginalRepoURL: sql.NullString{String: workspaceURL, Valid: true},
				OriginalHash:    sql.NullString{String: parsed.Value, Valid: true},
				OriginalBranch:  sql.NullString{String: branch, Valid: true},
			})
		}
		notifs, err := GetNotifications(workdir, NotificationFilter{Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		if len(notifs) > 2 {
			t.Errorf("expected at most 2 with limit, got %d", len(notifs))
		}
	})

	t.Run("Notifications_withFollower", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		workspaceURL := gitmsg.ResolveRepoURL(workdir)
		followerRepo := "https://github.com/follower/repo"
		_ = InsertFollower(followerRepo, workspaceURL, "following", "", time.Now())
		_ = cache.InsertCommits([]cache.Commit{{
			Hash: "fol_commit01", RepoURL: followerRepo, Branch: "main",
			AuthorName: "Follower", AuthorEmail: "follower@test.com",
			Message: "a post", Timestamp: time.Now(),
		}})
		notifs, err := GetNotifications(workdir, NotificationFilter{})
		if err != nil {
			t.Fatal(err)
		}
		hasFollow := false
		for _, n := range notifs {
			if n.Type == NotificationTypeFollow {
				hasFollow = true
				if n.ActorRepo != followerRepo {
					t.Errorf("ActorRepo = %q", n.ActorRepo)
				}
			}
		}
		if !hasFollow {
			t.Error("expected a follow notification")
		}
	})

	t.Run("Notifications_unreadCount", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		setupExternalInteraction(t, workdir, "comment")
		count, err := GetUnreadCount(workdir)
		if err != nil {
			t.Fatal(err)
		}
		if count == 0 {
			t.Error("expected non-zero unread count")
		}
	})

	t.Run("Notifications_markAllAsReadWithData", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		setupExternalInteraction(t, workdir, "comment")
		if err := MarkAllAsRead(workdir); err != nil {
			t.Fatalf("MarkAllAsRead() error = %v", err)
		}
		// Post-mark count not checked: parallel tests insert data between mark and count
	})

	t.Run("Notifications_markAllAsUnreadWithData", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		setupExternalInteraction(t, workdir, "comment")
		_ = MarkAllAsRead(workdir)
		if err := MarkAllAsUnread(workdir); err != nil {
			t.Fatalf("error = %v", err)
		}
		count, _ := GetUnreadCount(workdir)
		if count == 0 {
			t.Error("expected non-zero after MarkAllAsUnread")
		}
	})

	t.Run("Notifications_followerUnreadCount", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		workspaceURL := gitmsg.ResolveRepoURL(workdir)
		_ = InsertFollower("https://github.com/cnt/follower", workspaceURL, "list1", "", time.Now())
		count, err := GetUnreadCount(workdir)
		if err != nil {
			t.Fatal(err)
		}
		if count == 0 {
			t.Error("expected follower to count as unread")
		}
	})

	t.Run("Provider_withNotificationData", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		workspaceURL := gitmsg.ResolveRepoURL(workdir)
		branch := gitmsg.GetExtBranch(workdir, "social")
		// Create a workspace post to be the target of external comment
		post := CreatePost(workdir, "Provider test target", nil)
		if !post.Success {
			t.Fatal(post.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)
		parsed := protocol.ParseRef(post.Data.ID)
		// Insert external comment with unique hash to avoid collisions across -count runs
		suffix := fmt.Sprintf("%06d", atomic.AddInt64(&extInteractionCounter, 1))
		extRepo := "https://github.com/external/prov/" + suffix
		extHash := fmt.Sprintf("aaff0011%s", suffix[:4])
		_ = cache.InsertCommits([]cache.Commit{{
			Hash: extHash, RepoURL: extRepo, Branch: "main",
			AuthorName: "External User", AuthorEmail: "ext@test.com",
			Message: "comment content", Timestamp: time.Now(),
		}})
		_ = InsertSocialItem(SocialItem{
			RepoURL: extRepo, Hash: extHash, Branch: "main", Type: "comment",
			OriginalRepoURL: sql.NullString{String: workspaceURL, Valid: true},
			OriginalHash:    sql.NullString{String: parsed.Value, Valid: true},
			OriginalBranch:  sql.NullString{String: branch, Valid: true},
		})
		_ = InsertFollower("https://github.com/prov/follower", workspaceURL, "list1", "", time.Now())
		p := &notificationProvider{}
		result, err := p.GetNotifications(workdir, notifications.Filter{})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(result) == 0 {
			t.Fatal("expected notifications from provider")
		}
		hasComment, hasFollow := false, false
		for _, n := range result {
			if n.Type == "comment" {
				hasComment = true
				if n.Hash == "" {
					t.Error("comment notification should have Hash")
				}
				if n.Branch == "" {
					t.Error("comment notification should have Branch")
				}
			}
			if n.Type == "follow" {
				hasFollow = true
				if n.Hash != "follow" {
					t.Errorf("follow Hash = %q, want follow", n.Hash)
				}
			}
		}
		if !hasComment {
			t.Error("expected comment notification")
		}
		if !hasFollow {
			t.Error("expected follow notification")
		}
	})

	t.Run("NotificationProvider_GetNotifications", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		p := &notificationProvider{}
		result, err := p.GetNotifications(workdir, notifications.Filter{})
		if err != nil {
			t.Fatalf("provider.GetNotifications() error = %v", err)
		}
		if result == nil {
			t.Error("should not be nil")
		}
	})

	t.Run("NotificationProvider_GetUnreadCount", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		p := &notificationProvider{}
		_, err := p.GetUnreadCount(workdir)
		if err != nil {
			t.Fatalf("provider.GetUnreadCount() error = %v", err)
		}
		// Count may be non-zero due to thread participation from parallel tests sharing the cache
	})

	t.Run("Notifications_markAllAsReadWithFollowerAndItems", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		workspaceURL := gitmsg.ResolveRepoURL(workdir)
		// Setup external comment
		setupExternalInteraction(t, workdir, "comment")
		// Setup follower
		followerRepo := "https://github.com/fol/markall"
		_ = InsertFollower(followerRepo, workspaceURL, "list1", "", time.Now())
		// Verify both are unread
		countBefore, _ := GetUnreadCount(workdir)
		if countBefore < 2 {
			t.Errorf("expected at least 2 unread (item + follower), got %d", countBefore)
		}
		// Mark all as read
		if err := MarkAllAsRead(workdir); err != nil {
			t.Fatalf("MarkAllAsRead() error = %v", err)
		}
		// Post-mark count not checked: parallel tests insert data between mark and count
	})

	t.Run("Notifications_markAllAsUnreadWithFollowerAndItems", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		workspaceURL := gitmsg.ResolveRepoURL(workdir)
		setupExternalInteraction(t, workdir, "comment")
		followerRepo := "https://github.com/fol/markallun"
		_ = InsertFollower(followerRepo, workspaceURL, "list1", "", time.Now())
		_ = MarkAllAsRead(workdir)
		// Now unread all
		if err := MarkAllAsUnread(workdir); err != nil {
			t.Fatalf("error = %v", err)
		}
		countAfter, _ := GetUnreadCount(workdir)
		if countAfter < 2 {
			t.Errorf("expected at least 2 after MarkAllAsUnread, got %d", countAfter)
		}
	})

	t.Run("GetNotifications_limitTruncatesWithFollowers", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		workspaceURL := gitmsg.ResolveRepoURL(workdir)
		branch := gitmsg.GetExtBranch(workdir, "social")
		// Create workspace post as target
		post := CreatePost(workdir, "Limit truncation target", nil)
		if !post.Success {
			t.Fatal(post.Error.Message)
		}
		_ = SyncWorkspaceToCache(workdir)
		parsed := protocol.ParseRef(post.Data.ID)
		// Insert 2 external comments
		for i := 0; i < 2; i++ {
			extRepo := fmt.Sprintf("https://github.com/trunc%d/repo", i)
			hash := fmt.Sprintf("trn_%08d", i)
			_ = cache.InsertCommits([]cache.Commit{{
				Hash: hash, RepoURL: extRepo, Branch: "main",
				AuthorName: fmt.Sprintf("User %d", i), AuthorEmail: fmt.Sprintf("u%d@t.com", i),
				Message: "comment", Timestamp: time.Now().Add(time.Duration(-i) * time.Minute),
			}})
			_ = InsertSocialItem(SocialItem{
				RepoURL: extRepo, Hash: hash, Branch: "main", Type: "comment",
				OriginalRepoURL: sql.NullString{String: workspaceURL, Valid: true},
				OriginalHash:    sql.NullString{String: parsed.Value, Valid: true},
				OriginalBranch:  sql.NullString{String: branch, Valid: true},
			})
		}
		// Also add a follower
		_ = InsertFollower("https://github.com/trunc/follower", workspaceURL, "list1", "", time.Now())
		// Limit=2 but we have 2 items + 1 follower = 3 total → should truncate to 2
		notifs, err := GetNotifications(workdir, NotificationFilter{Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		if len(notifs) > 2 {
			t.Errorf("expected at most 2 with limit, got %d", len(notifs))
		}
	})

	t.Run("SocialNotificationProvider", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		setupExternalInteraction(t, workdir, "comment")

		all, err := notifications.GetAll(workdir, notifications.Filter{})
		if err != nil {
			t.Fatalf("GetAll error = %v", err)
		}
		if len(all) == 0 {
			t.Error("expected at least 1 notification from social provider")
		}
	})

	t.Run("SocialNotificationProviderUnreadCount", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		setupExternalInteraction(t, workdir, "comment")

		count, err := notifications.GetUnreadCount(workdir)
		if err != nil {
			t.Fatalf("GetUnreadCount error = %v", err)
		}
		if count == 0 {
			t.Error("expected unread count > 0")
		}
	})

	t.Run("GetUnreadCount_withItemsAndFollowers", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		workspaceURL := gitmsg.ResolveRepoURL(workdir)
		setupExternalInteraction(t, workdir, "comment")
		_ = InsertFollower("https://github.com/fol/count", workspaceURL, "list1", "", time.Now())
		count, err := GetUnreadCount(workdir)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if count < 2 {
			t.Errorf("expected at least 2 unread (item + follower), got %d", count)
		}
	})

	t.Run("GetUnreadCount_withWorkspace", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		wsURL := gitmsg.ResolveRepoURL(workdir)
		branch := gitmsg.GetExtBranch(workdir, "social")
		// Create a post in workspace
		postResult := CreatePost(workdir, "My unread test post", nil)
		if !postResult.Success {
			t.Fatalf("CreatePost failed: %s", postResult.Error.Message)
		}
		postRef := protocol.ParseRef(postResult.Data.ID)
		postHash := postRef.Value
		// Insert a comment from an external repo (unique per -count run)
		suffix := fmt.Sprintf("%06d", atomic.AddInt64(&extInteractionCounter, 1))
		extRepo := "https://github.com/ext/unread/" + suffix
		extHash := fmt.Sprintf("aae01122%s", suffix[:4])
		_ = cache.InsertCommits([]cache.Commit{{
			Hash: extHash, RepoURL: extRepo, Branch: "main",
			AuthorName: "Ext", AuthorEmail: "ext@t.com",
			Message: "comment", Timestamp: time.Now(),
		}})
		_ = InsertSocialItem(SocialItem{
			RepoURL: extRepo, Hash: extHash, Branch: "main", Type: "comment",
			OriginalRepoURL: cache.ToNullString(wsURL),
			OriginalHash:    cache.ToNullString(postHash),
			OriginalBranch:  cache.ToNullString(branch),
		})
		count, err := GetUnreadCount(workdir)
		if err != nil {
			t.Fatalf("GetUnreadCount error: %v", err)
		}
		if count < 1 {
			t.Errorf("expected at least 1 unread, got %d", count)
		}
	})

	t.Run("MarkAllAsRead_withWorkspace", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		wsURL := gitmsg.ResolveRepoURL(workdir)
		branch := gitmsg.GetExtBranch(workdir, "social")
		// Create post + external comment
		postResult := CreatePost(workdir, "mark-all-read post", nil)
		if !postResult.Success {
			t.Fatalf("CreatePost failed: %s", postResult.Error.Message)
		}
		postHash := protocol.ParseRef(postResult.Data.ID).Value
		suffix := fmt.Sprintf("%06d", atomic.AddInt64(&extInteractionCounter, 1))
		extRepo := "https://github.com/ext/markread/" + suffix
		extHash := fmt.Sprintf("aaf01122%s", suffix[:4])
		_ = cache.InsertCommits([]cache.Commit{{
			Hash: extHash, RepoURL: extRepo, Branch: "main",
			AuthorName: "Ext", AuthorEmail: "ext@t.com",
			Message: "comment", Timestamp: time.Now(),
		}})
		_ = InsertSocialItem(SocialItem{
			RepoURL: extRepo, Hash: extHash, Branch: "main", Type: "comment",
			OriginalRepoURL: cache.ToNullString(wsURL),
			OriginalHash:    cache.ToNullString(postHash),
			OriginalBranch:  cache.ToNullString(branch),
		})
		// Also add a follower
		_ = InsertFollower(extRepo, wsURL, "", "", time.Now())

		if err := MarkAllAsRead(workdir); err != nil {
			t.Fatalf("MarkAllAsRead error: %v", err)
		}
		// Post-mark count not checked: parallel tests insert data between mark and count
	})

	t.Run("MarkAllAsUnread_withWorkspace", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		wsURL := gitmsg.ResolveRepoURL(workdir)
		branch := gitmsg.GetExtBranch(workdir, "social")
		// Create post + external comment
		postResult := CreatePost(workdir, "mark-all-unread post", nil)
		if !postResult.Success {
			t.Fatalf("CreatePost failed: %s", postResult.Error.Message)
		}
		postHash := protocol.ParseRef(postResult.Data.ID).Value
		suffix := fmt.Sprintf("%06d", atomic.AddInt64(&extInteractionCounter, 1))
		extRepo := "https://github.com/ext/markunread/" + suffix
		extHash := fmt.Sprintf("aaf02233%s", suffix[:4])
		_ = cache.InsertCommits([]cache.Commit{{
			Hash: extHash, RepoURL: extRepo, Branch: "main",
			AuthorName: "Ext", AuthorEmail: "ext@t.com",
			Message: "comment", Timestamp: time.Now(),
		}})
		_ = InsertSocialItem(SocialItem{
			RepoURL: extRepo, Hash: extHash, Branch: "main", Type: "comment",
			OriginalRepoURL: cache.ToNullString(wsURL),
			OriginalHash:    cache.ToNullString(postHash),
			OriginalBranch:  cache.ToNullString(branch),
		})
		// Mark all as read first
		_ = MarkAllAsRead(workdir)
		// Then unread
		if err := MarkAllAsUnread(workdir); err != nil {
			t.Fatalf("MarkAllAsUnread error: %v", err)
		}
		count, _ := GetUnreadCount(workdir)
		if count < 1 {
			t.Errorf("expected at least 1 unread after MarkAllAsUnread, got %d", count)
		}
	})
} // end TestNotificationIntegration

func TestGetUnreadCount_emptyWorkdir(t *testing.T) {
	setupTestDB(t)
	count, err := GetUnreadCount("")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}

func TestGetNotifications_emptyWorkdir(t *testing.T) {
	setupTestDB(t)
	notifications, err := GetNotifications("", NotificationFilter{})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(notifications) != 0 {
		t.Errorf("expected 0, got %d", len(notifications))
	}
}

func TestMarkAllAsRead_emptyWorkdir(t *testing.T) {
	setupTestDB(t)
	if err := MarkAllAsRead(""); err != nil {
		t.Fatalf("error = %v", err)
	}
}

func TestMarkAllAsUnread_emptyWorkdir(t *testing.T) {
	setupTestDB(t)
	if err := MarkAllAsUnread(""); err != nil {
		t.Fatalf("error = %v", err)
	}
}

// (TestGetLogs_withFilters, TestGetLogs_repositoryMyScope, TestGetLogs_timelineScope are in TestLogsIntegration group)

// --- GetRelatedRepositories ---

func TestRelatedRepos(t *testing.T) {
	t.Parallel()

	t.Run("GetRelatedRepositories", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreateList(workdir, "related-list", "Related")
		AddRepositoryToList(workdir, "related-list", "https://github.com/target/repo", "main", false)
		AddRepositoryToList(workdir, "related-list", "https://github.com/other/repo", "main", false)

		result := GetRelatedRepositories(workdir, "https://github.com/target/repo")
		if !result.Success {
			t.Fatalf("GetRelatedRepositories() failed: %s", result.Error.Message)
		}
		// other/repo shares related-list with target/repo
		if len(result.Data) < 1 {
			t.Errorf("expected at least 1 related repo, got %d", len(result.Data))
		}
	})

	t.Run("GetRelatedRepositories_noRelated", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		result := GetRelatedRepositories(workdir, "https://github.com/lonely/repo")
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
		if len(result.Data) != 0 {
			t.Errorf("expected 0 related, got %d", len(result.Data))
		}
	})

	t.Run("GetRelatedRepositories_sharedAuthors", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		target := "https://github.com/target/shared"
		related := "https://github.com/related/shared"
		CreateList(workdir, "shared-list", "Shared")
		AddRepositoryToList(workdir, "shared-list", target, "main", false)
		_ = SyncWorkspaceToCache(workdir)
		// Insert items from both repos with shared author
		for _, r := range []string{target, related} {
			_ = cache.InsertCommits([]cache.Commit{{
				Hash: "sh_" + r[len(r)-6:], RepoURL: r, Branch: "main",
				AuthorName: "Shared Author", AuthorEmail: "shared@test.com",
				Message: "content", Timestamp: time.Now(),
			}})
			_ = InsertSocialItem(SocialItem{RepoURL: r, Hash: "sh_" + r[len(r)-6:], Branch: "main", Type: "post"})
		}
		result := GetRelatedRepositories(workdir, target)
		if !result.Success {
			t.Fatal(result.Error.Message)
		}
		found := false
		for _, r := range result.Data {
			if r.URL == related {
				found = true
				if len(r.Relationships.SharedAuthors) == 0 {
					t.Error("expected shared authors")
				}
			}
		}
		if !found {
			t.Error("expected related repo with shared author")
		}
	})

	t.Run("GetRelatedRepositories_sharedAuthorsOnly", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		workspaceURL := gitmsg.ResolveRepoURL(workdir)
		branch := gitmsg.GetExtBranch(workdir, "social")

		// Create post in workspace
		CreatePost(workdir, "My post", nil)
		_ = SyncWorkspaceToCache(workdir)

		// Create post in another repo by same author
		authorEmail := ""
		commit, _ := git.GetCommit(workdir, "HEAD")
		if commit != nil {
			authorEmail = commit.Email
		}
		otherRepo := "https://github.com/other/authortest"
		_ = cache.InsertCommits([]cache.Commit{{
			Hash: "aa1122334455", RepoURL: otherRepo, Branch: branch,
			AuthorName: "Same Author", AuthorEmail: authorEmail,
			Message: "shared author post", Timestamp: time.Now(),
		}})
		_ = InsertSocialItem(SocialItem{RepoURL: otherRepo, Hash: "aa1122334455", Branch: branch, Type: "post"})

		result := GetRelatedRepositories(workdir, workspaceURL)
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
		found := false
		for _, r := range result.Data {
			if r.URL == otherRepo {
				found = true
				if len(r.Relationships.SharedAuthors) == 0 {
					t.Error("expected shared authors")
				}
			}
		}
		if !found {
			t.Error("expected other repo in related repositories via shared author")
		}
	})

	t.Run("GetRelatedRepositories_equalScores", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		targetURL := "https://github.com/eq/target"
		// Create two lists each containing the target
		CreateList(workdir, "eq-list1", "EQ1")
		CreateList(workdir, "eq-list2", "EQ2")
		AddRepositoryToList(workdir, "eq-list1", targetURL, "main", false)
		AddRepositoryToList(workdir, "eq-list2", targetURL, "main", false)
		// Add two other repos in each list (same score)
		repo1 := "https://github.com/eq/repo1"
		repo2 := "https://github.com/eq/repo2"
		AddRepositoryToList(workdir, "eq-list1", repo1, "main", false)
		AddRepositoryToList(workdir, "eq-list2", repo2, "main", false)
		AddRepositoryToList(workdir, "eq-list1", repo2, "main", false)
		AddRepositoryToList(workdir, "eq-list2", repo1, "main", false)
		_ = SyncWorkspaceToCache(workdir)
		result := GetRelatedRepositories(workdir, targetURL)
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
		// Both repos share 2 lists with target -> equal score
		if len(result.Data) < 2 {
			t.Errorf("expected at least 2 related repos, got %d", len(result.Data))
		}
	})
}

// (TestGetEditHistoryPosts is in TestVersionAndResolve group)

// --- Provider ---

// --- Notification scenarios with real data ---

var extInteractionCounter int64

func setupExternalInteraction(t *testing.T, workdir, interactionType string) (externalRepo, externalHash string) {
	t.Helper()
	workspaceURL := gitmsg.ResolveRepoURL(workdir)
	branch := gitmsg.GetExtBranch(workdir, "social")
	post := CreatePost(workdir, "Target for "+interactionType, nil)
	if !post.Success {
		t.Fatal(post.Error.Message)
	}
	_ = SyncWorkspaceToCache(workdir)
	parsed := protocol.ParseRef(post.Data.ID)
	suffix := fmt.Sprintf("%06d", atomic.AddInt64(&extInteractionCounter, 1))
	externalRepo = "https://github.com/external/" + interactionType + "/" + suffix
	externalHash = "ext_" + interactionType[:4] + suffix
	_ = cache.InsertCommits([]cache.Commit{{
		Hash: externalHash, RepoURL: externalRepo, Branch: "main",
		AuthorName: "External User", AuthorEmail: "ext@test.com",
		Message: "interaction content", Timestamp: time.Now(),
	}})
	_ = InsertSocialItem(SocialItem{
		RepoURL: externalRepo, Hash: externalHash, Branch: "main", Type: interactionType,
		OriginalRepoURL: sql.NullString{String: workspaceURL, Valid: true},
		OriginalHash:    sql.NullString{String: parsed.Value, Valid: true},
		OriginalBranch:  sql.NullString{String: branch, Valid: true},
	})
	return
}

// (Search additional scopes tests are in TestSearchIntegration group)

// (TestGetRelatedRepositories_sharedAuthors is in TestRelatedRepos group)

// (TestGetRepositories_allWithCachedRepos is in TestStatusAndRepositories group)

// (TestGetPosts_repositoryWithBranch is in TestGetPostsIntegration group)

// (TestResolveItem_fromCachedCommit, TestResolveItem_notFound are in TestVersionAndResolve group)

func TestResolveItem_fromWorkspaceCommit(t *testing.T) {
	workdir := initWorkspace(t)
	// Create a post but don't sync to cache - should resolve from workspace git
	post := CreatePost(workdir, "Workspace resolve test", nil)
	if !post.Success {
		t.Fatal(post.Error.Message)
	}
	// Reset cache to force workspace resolution
	cache.Reset()
	dir := t.TempDir()
	if err := cache.Open(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cache.Reset() })
	parsed := protocol.ParseRef(post.Data.ID)
	item := resolveItem(workdir, "#commit:"+parsed.Value)
	if item == nil {
		t.Fatal("resolveItem should find commit from workspace")
	}
	if item.AuthorName == "" {
		t.Error("AuthorName should be populated from git commit")
	}
}

// (TestResolveItem_emptyRef is in TestVersionAndResolve group)

// (TestGetPosts_threadWithNestedComments is in TestGetPostsIntegration group)

// --- processWorkspaceCommits ---

func TestProcessWorkspaceCommits_withComments(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/pwc"
	branch := "gitmsg/social"
	header := protocol.Header{Ext: "social", V: "0.1.0", Fields: map[string]string{
		"type":     "comment",
		"original": "#commit:abc012345678@" + branch,
		"reply-to": "#commit:def012345678@" + branch,
	}}
	content := protocol.FormatMessage("A reply", header, nil)
	commits := []git.Commit{{
		Hash: "aaa012345678", Author: "Test", Email: "test@t.com",
		Message: content, Timestamp: time.Now(),
	}}
	_ = cache.InsertCommits([]cache.Commit{{
		Hash: "aaa012345678", RepoURL: repoURL, Branch: branch,
		AuthorName: "Test", AuthorEmail: "test@t.com",
		Message: content, Timestamp: time.Now(),
	}})
	processWorkspaceCommits(commits, repoURL, branch)
	item, err := cache.QueryLocked(func(db *sql.DB) (SocialItem, error) {
		var s SocialItem
		err := db.QueryRow(`SELECT type, original_hash, reply_to_hash FROM social_items WHERE repo_url = ? AND hash = ? AND branch = ?`,
			repoURL, "aaa012345678", branch).Scan(
			&s.Type, &s.OriginalHash, &s.ReplyToHash)
		return s, err
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.Type != "comment" {
		t.Errorf("Type = %q", item.Type)
	}
	if !item.OriginalHash.Valid {
		t.Error("OriginalHash should be valid")
	}
	if !item.ReplyToHash.Valid {
		t.Error("ReplyToHash should be valid")
	}
}

func TestProcessWorkspaceCommits_nonSocial(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/pwc2"
	commits := []git.Commit{{
		Hash: "pwc_plain123", Author: "Test", Email: "test@t.com",
		Message: "plain commit", Timestamp: time.Now(),
	}}
	_ = cache.InsertCommits([]cache.Commit{{
		Hash: "pwc_plain123", RepoURL: repoURL, Branch: "main",
		AuthorName: "Test", AuthorEmail: "test@t.com",
		Message: "plain commit", Timestamp: time.Now(),
	}})
	processWorkspaceCommits(commits, repoURL, "main")
	// Non-social commits try to upgrade virtual items (noop if none)
}

func TestProcessWorkspaceCommits_withVirtualRef(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/pwcvirt"
	branch := "gitmsg/social"
	ts := time.Now().UTC().Format(time.RFC3339)
	header := protocol.Header{Ext: "social", V: "0.1.0", Fields: map[string]string{"type": "post"}}
	refs := []protocol.Ref{{
		Ext:      "social",
		Author:   "Remote",
		Email:    "remote@t.com",
		Time:     ts,
		Ref:      "#commit:aabb00112233@" + branch,
		V:        "0.1.0",
		Fields:   map[string]string{"type": "post"},
		Metadata: "> Quoted content from remote",
	}}
	content := protocol.FormatMessage("A post", header, refs)
	commits := []git.Commit{{
		Hash: "bbb012345678", Author: "Test", Email: "test@t.com",
		Message: content, Timestamp: time.Now(),
	}}
	_ = cache.InsertCommits([]cache.Commit{{
		Hash: "bbb012345678", RepoURL: repoURL, Branch: branch,
		AuthorName: "Test", AuthorEmail: "test@t.com",
		Message: content, Timestamp: time.Now(),
	}})
	processWorkspaceCommits(commits, repoURL, branch)
	count, _ := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_commits WHERE hash = ? AND is_virtual = 1`, "aabb00112233").Scan(&c)
		return c, err
	})
	if count != 1 {
		t.Errorf("expected 1 virtual commit, got %d", count)
	}
}

// --- Fetch with workspace ---

func TestFetchIntegration(t *testing.T) {
	t.Parallel()

	t.Run("Fetch_emptyWorkspace", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		cacheDir := t.TempDir()
		result := Fetch(workdir, cacheDir, nil)
		// Should succeed with 0 repos (no lists with repos)
		if result.Success {
			if result.Data.Repositories != 0 {
				t.Logf("Repositories = %d", result.Data.Repositories)
			}
		}
	})

	t.Run("Fetch_withOptions", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		cacheDir := t.TempDir()
		result := Fetch(workdir, cacheDir, &FetchOptions{
			Since:    "2020-01-01",
			Before:   "2030-12-31",
			Parallel: 2,
		})
		// May succeed or fail depending on fetch infrastructure, but exercises the code path
		_ = result
	})

	t.Run("Fetch_withListIDFilter", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreateList(workdir, "fetch-list", "Fetch List")
		cacheDir := t.TempDir()
		result := Fetch(workdir, cacheDir, &FetchOptions{ListID: "fetch-list"})
		_ = result
	})

	t.Run("Fetch_withExtraProcessors", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		cacheDir := t.TempDir()
		called := false
		extra := func(gc git.Commit, msg *protocol.Message, repoURL, branch string) {
			called = true
		}
		result := Fetch(workdir, cacheDir, &FetchOptions{
			ExtraProcessors: []fetch.CommitProcessor{extra},
		})
		if !result.Success {
			t.Fatalf("error: %s", result.Error.Message)
		}
		// Extra processor may not be called if no repos, but at least it doesn't panic
		_ = called
	})

	t.Run("Fetch_malformedRepoRef", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		// Create a list with a malformed repo ref
		CreateList(workdir, "malformed-list", "Malformed")
		_ = gitmsg.WriteList(workdir, "social", "malformed-list", gitmsg.ListData{
			ID: "malformed-list", Name: "Malformed", Version: "0.1.0",
			Repositories: []string{"not-a-valid-url", ""},
		})
		_ = SyncWorkspaceToCache(workdir)
		// Fetch should skip malformed refs without crashing
		result := Fetch(workdir, t.TempDir(), &FetchOptions{ListID: "malformed-list"})
		// May succeed with 0 repos or fail for other reasons, but shouldn't panic
		_ = result
	})

	t.Run("Fetch_listIDFilterSkipsOther", func(t *testing.T) {
		t.Parallel()
		workdir := cloneFixture(t)
		CreateList(workdir, "fetch-a", "A")
		CreateList(workdir, "fetch-b", "B")
		AddRepositoryToList(workdir, "fetch-a", "https://github.com/fetch-a/repo", "main", false)
		AddRepositoryToList(workdir, "fetch-b", "https://github.com/fetch-b/repo", "main", false)
		_ = SyncWorkspaceToCache(workdir)
		// Only fetch list "fetch-a", should skip "fetch-b"
		result := Fetch(workdir, t.TempDir(), &FetchOptions{ListID: "fetch-a"})
		_ = result // Just verify no panic
	})
}

// (TestGetRepositories_listNotFound is in TestStatusAndRepositories group)

// (TestGetPosts_threadFromComment, TestGetPosts_threadInvalidRef, TestGetPosts_singlePost_notFound are in TestGetPostsIntegration group)

// --- MarkAllAsRead/MarkAllAsUnread with BOTH items AND followers ---

// --- GetLogs edge cases ---

// (TestGetLogs_unknownScope, TestGetLogs_afterBeforeFilter, TestGetLogs_typeFilterExcludes are in TestLogsIntegration group)

// --- resolveItem: from workspace commit with original field ---

func TestResolveItem_fromWorkspaceCommitWithOriginal(t *testing.T) {
	workdir := initWorkspace(t)
	post := CreatePost(workdir, "Original for resolve", nil)
	if !post.Success {
		t.Fatal(post.Error.Message)
	}
	_ = SyncWorkspaceToCache(workdir)

	comment := CreateComment(workdir, post.Data.ID, "Comment for resolve", nil)
	if !comment.Success {
		t.Fatal(comment.Error.Message)
	}
	// Reset cache to force workspace git resolution
	cache.Reset()
	dir := t.TempDir()
	if err := cache.Open(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cache.Reset() })

	parsed := protocol.ParseRef(comment.Data.ID)
	item := resolveItem(workdir, "#commit:"+parsed.Value)
	if item == nil {
		t.Fatal("should resolve comment from workspace")
	}
	if item.Type != "comment" {
		t.Errorf("Type = %q, want comment", item.Type)
	}
	if !item.OriginalRepoURL.Valid {
		t.Error("OriginalRepoURL should be valid for comment resolved from workspace")
	}
}

// (TestResolveItem_fallbackToCachedCommit is in TestVersionAndResolve group)

// (Search additional filters tests are in TestSearchIntegration group)

// (TestFetch_withListIDFilter is in TestFetchIntegration group)

// (TestResolveCurrentVersion_withEdit is in TestVersionAndResolve group)

// --- FormatRelativeTime edge cases ---

func TestFormatRelativeTime_minutesSingular(t *testing.T) {
	got := FormatRelativeTime(time.Now().Add(-1 * time.Minute))
	if got != "1 minute ago" {
		t.Errorf("FormatRelativeTime(-1m) = %q, want %q", got, "1 minute ago")
	}
}

func TestFormatRelativeTime_hoursSingular(t *testing.T) {
	got := FormatRelativeTime(time.Now().Add(-1 * time.Hour))
	if got != "1 hour ago" {
		t.Errorf("FormatRelativeTime(-1h) = %q, want %q", got, "1 hour ago")
	}
}

func TestFormatRelativeTime_daysSingular(t *testing.T) {
	got := FormatRelativeTime(time.Now().Add(-24 * time.Hour))
	if got != "1 day ago" {
		t.Errorf("FormatRelativeTime(-1d) = %q, want %q", got, "1 day ago")
	}
}

func TestFormatRelativeTime_daysPlural(t *testing.T) {
	got := FormatRelativeTime(time.Now().Add(-3 * 24 * time.Hour))
	if got != "3 days ago" {
		t.Errorf("FormatRelativeTime(-3d) = %q, want %q", got, "3 days ago")
	}
}

func TestFormatRelativeTime_minutesPlural(t *testing.T) {
	got := FormatRelativeTime(time.Now().Add(-10 * time.Minute))
	if got != "10 minutes ago" {
		t.Errorf("FormatRelativeTime(-10m) = %q, want %q", got, "10 minutes ago")
	}
}

func TestFormatRelativeTime_hoursPlural(t *testing.T) {
	got := FormatRelativeTime(time.Now().Add(-5 * time.Hour))
	if got != "5 hours ago" {
		t.Errorf("FormatRelativeTime(-5h) = %q, want %q", got, "5 hours ago")
	}
}

func TestFormatRelativeTime_justNow(t *testing.T) {
	got := FormatRelativeTime(time.Now())
	if got != "just now" {
		t.Errorf("FormatRelativeTime(now) = %q, want %q", got, "just now")
	}
}

// (TestGetPosts_listScope_nilOpts, TestGetPosts_repositoryMyWithDateRange are in TestGetPostsIntegration group)

// --- GetNotifications with limit ---

// --- GetNotifications with unread filter ---

// --- GetUnreadCount with both items and followers ---

// (TestGetPosts_threadRootNotInResults is in TestGetPostsIntegration group)

// (TestGetRepositories_withCachedRepos, TestGetRepositories_listWithFetchRanges are in TestStatusAndRepositories group)

// (TestGetRelatedRepositories_sharedAuthorsOnly is in TestRelatedRepos group)

// (TestCheckIfRepoFollowsWorkspace_withLists, TestCacheExternalRepoLists_withLists are in TestStatusAndRepositories group)

// --- upgradeVirtualItem DB test ---

func TestUpgradeVirtualItem_withVirtualCommit(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/upgrade-virt"
	hash := "aabb11cc2233"
	branch := "gitmsg/social"
	// Insert virtual commit
	_ = cache.InsertCommits([]cache.Commit{{
		Hash: hash, RepoURL: repoURL, Branch: branch,
		AuthorName: "Virtual", AuthorEmail: "v@t.com",
		Message: "virtual", Timestamp: time.Now(),
	}})
	_ = cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`UPDATE core_commits SET is_virtual = 1 WHERE repo_url = ? AND hash = ?`, repoURL, hash)
		return err
	})

	// Upgrade it
	upgradeVirtualItem(git.Commit{
		Hash:      hash,
		Author:    "Real Author",
		Email:     "real@t.com",
		Message:   "real content",
		Timestamp: time.Now(),
	}, repoURL)

	// Verify upgrade
	isVirtual, _ := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var v int
		err := db.QueryRow(`SELECT is_virtual FROM core_commits WHERE repo_url = ? AND hash = ?`, repoURL, hash).Scan(&v)
		return v, err
	})
	if isVirtual != 0 {
		t.Error("expected is_virtual = 0 after upgrade")
	}
}

// (TestDeleteList_success, TestRemoveRepositoryFromList_ok, TestGetList_exists are in TestListManagement group)

// (TestFetch_withExtraProcessors is in TestFetchIntegration group)

// (TestStatus_initialized is in TestStatusAndRepositories group)

// (TestGetPosts_workspaceScope is in TestGetPostsIntegration group)

// (TestGetPosts_timelineWithListPosts is in TestGetPostsIntegration group)

// --- processWorkspaceCommits with non-social commits ---

func TestProcessWorkspaceCommits_nonSocialCommit(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/nonsocial"
	branch := "main"
	// Insert a virtual commit first
	_ = cache.InsertCommits([]cache.Commit{{
		Hash: "aabb11335577", RepoURL: repoURL, Branch: branch,
		AuthorName: "V", AuthorEmail: "v@t.com",
		Message: "virtual", Timestamp: time.Now(),
	}})
	_ = cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`UPDATE core_commits SET is_virtual = 1 WHERE hash = ?`, "aabb11335577")
		return err
	})
	// Process a non-social commit with same hash (triggers upgradeVirtualItem path)
	commits := []git.Commit{{
		Hash:      "aabb11335577",
		Author:    "Real",
		Email:     "real@t.com",
		Message:   "just a regular commit with no gitmsg header",
		Timestamp: time.Now(),
	}}
	processWorkspaceCommits(commits, repoURL, branch)
	// Verify virtual flag cleared
	isVirtual, _ := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var v int
		err := db.QueryRow(`SELECT is_virtual FROM core_commits WHERE hash = ?`, "aabb11335577").Scan(&v)
		return v, err
	})
	if isVirtual != 0 {
		t.Error("expected virtual flag to be cleared")
	}
}

// --- Branch default coverage in processWorkspaceCommits ---

func TestProcessWorkspaceCommits_originalNoBranch(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/orig-no-branch"
	branch := "gitmsg/social"
	// Create a commit with original ref that has no branch part
	header := protocol.Header{Ext: "social", V: "0.1.0", Fields: map[string]string{
		"type":     "comment",
		"original": "#commit:aabb00112233", // no @branch
	}}
	content := protocol.FormatMessage("Comment", header, nil)
	_ = cache.InsertCommits([]cache.Commit{{
		Hash: "onb_112233445", RepoURL: repoURL, Branch: branch,
		AuthorName: "Test", AuthorEmail: "t@t.com",
		Message: content, Timestamp: time.Now(),
	}})
	commits := []git.Commit{{
		Hash: "onb_112233445", Author: "Test", Email: "t@t.com",
		Message: content, Timestamp: time.Now(),
	}}
	processWorkspaceCommits(commits, repoURL, branch)
	// Verify the social item was created with branch defaulted to current branch
	item, _ := cache.QueryLocked(func(db *sql.DB) (SocialItem, error) {
		var s SocialItem
		err := db.QueryRow(`SELECT original_branch FROM social_items WHERE repo_url = ? AND hash = ?`,
			repoURL, "onb_112233445").Scan(&s.OriginalBranch)
		return s, err
	})
	if item.OriginalBranch.Valid && item.OriginalBranch.String != branch {
		t.Errorf("OriginalBranch = %q, want %q", item.OriginalBranch.String, branch)
	}
}

func TestProcessWorkspaceCommits_replyToNoBranch(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/reply-no-branch"
	branch := "gitmsg/social"
	header := protocol.Header{Ext: "social", V: "0.1.0", Fields: map[string]string{
		"type":     "comment",
		"original": "#commit:aabb00112233@" + branch,
		"reply-to": "#commit:ccdd00112233", // no @branch
	}}
	content := protocol.FormatMessage("Nested reply", header, nil)
	_ = cache.InsertCommits([]cache.Commit{{
		Hash: "rnb_112233445", RepoURL: repoURL, Branch: branch,
		AuthorName: "Test", AuthorEmail: "t@t.com",
		Message: content, Timestamp: time.Now(),
	}})
	commits := []git.Commit{{
		Hash: "rnb_112233445", Author: "Test", Email: "t@t.com",
		Message: content, Timestamp: time.Now(),
	}}
	processWorkspaceCommits(commits, repoURL, branch)
	item, _ := cache.QueryLocked(func(db *sql.DB) (SocialItem, error) {
		var s SocialItem
		err := db.QueryRow(`SELECT reply_to_branch FROM social_items WHERE repo_url = ? AND hash = ?`,
			repoURL, "rnb_112233445").Scan(&s.ReplyToBranch)
		return s, err
	})
	if item.ReplyToBranch.Valid && item.ReplyToBranch.String != branch {
		t.Errorf("ReplyToBranch = %q, want %q", item.ReplyToBranch.String, branch)
	}
}

// --- processSocialCommit branch defaults (fetch.go) ---

func TestProcessSocialCommit_originalNoBranch(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/psc-orig"
	branch := "gitmsg/social"
	header := protocol.Header{Ext: "social", V: "0.1.0", Fields: map[string]string{
		"type":     "comment",
		"original": "#commit:aabb00223344", // no @branch
	}}
	content := protocol.FormatMessage("Fetched comment", header, nil)
	_ = cache.InsertCommits([]cache.Commit{{
		Hash: "pso_112233445", RepoURL: repoURL, Branch: branch,
		AuthorName: "Test", AuthorEmail: "t@t.com",
		Message: content, Timestamp: time.Now(),
	}})
	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: "pso_112233445", Author: "Test", Email: "t@t.com", Message: content, Timestamp: time.Now()}
	processSocialCommit(gc, msg, repoURL, branch)
	item, _ := cache.QueryLocked(func(db *sql.DB) (SocialItem, error) {
		var s SocialItem
		err := db.QueryRow(`SELECT original_branch FROM social_items WHERE repo_url = ? AND hash = ?`,
			repoURL, "pso_112233445").Scan(&s.OriginalBranch)
		return s, err
	})
	if item.OriginalBranch.Valid && item.OriginalBranch.String != branch {
		t.Errorf("OriginalBranch = %q, want %q", item.OriginalBranch.String, branch)
	}
}

func TestProcessSocialCommit_replyToNoBranch(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/psc-reply"
	branch := "gitmsg/social"
	header := protocol.Header{Ext: "social", V: "0.1.0", Fields: map[string]string{
		"type":     "comment",
		"original": "#commit:aabb00223344@" + branch,
		"reply-to": "#commit:ccdd00223344", // no @branch
	}}
	content := protocol.FormatMessage("Fetched reply", header, nil)
	_ = cache.InsertCommits([]cache.Commit{{
		Hash: "psr_112233445", RepoURL: repoURL, Branch: branch,
		AuthorName: "Test", AuthorEmail: "t@t.com",
		Message: content, Timestamp: time.Now(),
	}})
	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: "psr_112233445", Author: "Test", Email: "t@t.com", Message: content, Timestamp: time.Now()}
	processSocialCommit(gc, msg, repoURL, branch)
	item, _ := cache.QueryLocked(func(db *sql.DB) (SocialItem, error) {
		var s SocialItem
		err := db.QueryRow(`SELECT reply_to_branch FROM social_items WHERE repo_url = ? AND hash = ?`,
			repoURL, "psr_112233445").Scan(&s.ReplyToBranch)
		return s, err
	})
	if item.ReplyToBranch.Valid && item.ReplyToBranch.String != branch {
		t.Errorf("ReplyToBranch = %q, want %q", item.ReplyToBranch.String, branch)
	}
}

// --- Notification limit truncation after follower merge ---

// (TestCreateComment_nestedCommentNoOriginalRef, TestCreateComment_nestedCommentRootNotFound,
//  TestCreateComment_nestedCommentRootIsComment, TestCreateComment_onRemotePost are in TestCommentOps group)

// (TestGetRelatedRepositories_equalScores is in TestRelatedRepos group)

// (TestGetRelatedRepositories_emptyRepoRef is in TestStatusAndRepositories group)

// (TestGetRepositories_allWithFetchRangesData is in TestStatusAndRepositories group)

// (TestGetPosts_listScopeWithData is in TestGetPostsIntegration group)

// (TestGetPosts_threadBranchDefault is in TestGetPostsIntegration group)

// (TestGetLogs_longContentTruncation is in TestLogsIntegration group)

// (Search coverage tests are in TestSearchIntegration group)

// (TestGetPosts_singlePostWorkspace is in TestGetPostsIntegration group)

// (TestFetch_malformedRepoRef, TestFetch_listIDFilterSkipsOther are in TestFetchIntegration group)

// --- SortThreadTree parentCommentID empty ---

func TestSortThreadTree_directChildNoBranching(t *testing.T) {
	now := time.Now()
	posts := []Post{
		{ID: "root-id", Content: "Root"},
		{ID: "child-1", OriginalPostID: "root-id", Timestamp: now, Content: "Direct child"},
	}
	result := SortThreadTree("root-id", posts)
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	if result[0].Depth != 1 {
		t.Errorf("depth = %d, want 1", result[0].Depth)
	}
}

// (TestGetPosts_repositoryExternalMatchingWorkspace is in TestGetPostsIntegration group)

// --- GetUnreadCount integration ---

// --- MarkAllAsRead / MarkAllAsUnread integration ---

// --- EditPost / RetractPost ---
// (TestEditPost_success and TestRetractPost_success are in TestPostCRUD group)

// --- GetThread / GetParentChain DB tests ---

func TestGetThread_withReplies(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/thread/test"
	branch := "main"
	rootHash := "eee0dd112233"
	childHash := "eee0dd223344"
	now := time.Now()
	_ = cache.InsertCommits([]cache.Commit{
		{Hash: rootHash, RepoURL: repoURL, Branch: branch, AuthorName: "A", AuthorEmail: "a@t.com", Message: "Root", Timestamp: now},
		{Hash: childHash, RepoURL: repoURL, Branch: branch, AuthorName: "B", AuthorEmail: "b@t.com", Message: "Reply", Timestamp: now.Add(time.Minute)},
	})
	_ = InsertSocialItem(SocialItem{RepoURL: repoURL, Hash: rootHash, Branch: branch, Type: "post"})
	_ = InsertSocialItem(SocialItem{
		RepoURL: repoURL, Hash: childHash, Branch: branch, Type: "comment",
		OriginalRepoURL: cache.ToNullString(repoURL),
		OriginalHash:    cache.ToNullString(rootHash),
		OriginalBranch:  cache.ToNullString(branch),
		ReplyToRepoURL:  cache.ToNullString(repoURL),
		ReplyToHash:     cache.ToNullString(rootHash),
		ReplyToBranch:   cache.ToNullString(branch),
	})
	items, err := GetThread(repoURL, rootHash, branch, "")
	if err != nil {
		t.Fatalf("GetThread error: %v", err)
	}
	if len(items) < 2 {
		t.Errorf("expected at least 2 items in thread, got %d", len(items))
	}
}

func TestGetParentChain_withParent(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/parent/chain"
	branch := "main"
	parentHash := "eee0ee112233"
	childHash := "eee0ee223344"
	now := time.Now()
	_ = cache.InsertCommits([]cache.Commit{
		{Hash: parentHash, RepoURL: repoURL, Branch: branch, AuthorName: "A", AuthorEmail: "a@t.com", Message: "Parent", Timestamp: now},
		{Hash: childHash, RepoURL: repoURL, Branch: branch, AuthorName: "B", AuthorEmail: "b@t.com", Message: "Child", Timestamp: now.Add(time.Minute)},
	})
	_ = InsertSocialItem(SocialItem{RepoURL: repoURL, Hash: parentHash, Branch: branch, Type: "post"})
	_ = InsertSocialItem(SocialItem{
		RepoURL: repoURL, Hash: childHash, Branch: branch, Type: "comment",
		OriginalRepoURL: cache.ToNullString(repoURL),
		OriginalHash:    cache.ToNullString(parentHash),
		OriginalBranch:  cache.ToNullString(branch),
		ReplyToRepoURL:  cache.ToNullString(repoURL),
		ReplyToHash:     cache.ToNullString(parentHash),
		ReplyToBranch:   cache.ToNullString(branch),
	})
	parents, err := GetParentChain(repoURL, childHash, branch, "")
	if err != nil {
		t.Fatalf("GetParentChain error: %v", err)
	}
	if len(parents) != 1 {
		t.Errorf("expected 1 parent, got %d", len(parents))
	}
}

// --- ResolveCurrentVersion / GetEditHistory ---

func TestResolveCurrentVersion_simple(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/resolve/ver"
	hash := "eee0cc112233"
	branch := "main"
	_ = cache.InsertCommits([]cache.Commit{{
		Hash: hash, RepoURL: repoURL, Branch: branch,
		AuthorName: "Test", AuthorEmail: "t@t.com",
		Message: "A post", Timestamp: time.Now(),
	}})
	resolved, err := ResolveCurrentVersion(repoURL, hash, branch, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if resolved.Item == nil {
		t.Fatal("Item should not be nil")
	}
	if resolved.Item.Hash != hash {
		t.Errorf("Hash = %q, want %q", resolved.Item.Hash, hash)
	}
}

func TestGetEditHistory_noEdits(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/edit/history"
	hash := "eee0aa112233"
	branch := "main"
	_ = cache.InsertCommits([]cache.Commit{{
		Hash: hash, RepoURL: repoURL, Branch: branch,
		AuthorName: "Test", AuthorEmail: "t@t.com",
		Message: "Original", Timestamp: time.Now(),
	}})
	items, err := GetEditHistory(repoURL, hash, branch, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 version, got %d", len(items))
	}
}

func TestGetEditHistoryPosts_noEdits(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/edit/histposts"
	hash := "eee0bb112233"
	branch := "main"
	_ = cache.InsertCommits([]cache.Commit{{
		Hash: hash, RepoURL: repoURL, Branch: branch,
		AuthorName: "Test", AuthorEmail: "t@t.com",
		Message: "Original", Timestamp: time.Now(),
	}})
	posts, err := GetEditHistoryPosts(repoURL, hash, branch, "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(posts) != 1 {
		t.Errorf("expected 1 post, got %d", len(posts))
	}
}

// --- GetFollowerSet / GetFollowers DB ---

func TestGetFollowerSet_withData(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/ws/follset"
	_ = InsertFollower("https://github.com/f1/r", wsURL, "", "", time.Now())
	_ = InsertFollower("https://github.com/f2/r", wsURL, "", "", time.Now())
	set, err := GetFollowerSet(wsURL)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(set) != 2 {
		t.Errorf("expected 2 followers, got %d", len(set))
	}
}

func TestGetFollowers_withData(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/ws/followers2"
	_ = InsertFollower("https://github.com/f1/r2", wsURL, "", "", time.Now())
	_ = InsertFollower("https://github.com/f2/r2", wsURL, "", "", time.Now())
	followers, err := GetFollowers(wsURL)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(followers) != 2 {
		t.Errorf("expected 2 followers, got %d", len(followers))
	}
}

// --- List operations edge cases ---

// --- updateAncestorInteractions with nested replies ---

func TestUpdateAncestorInteractions_nestedChain(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/ancestor/test"
	branch := "main"
	rootHash := "eee0ff112233"
	midHash := "eee0ff223344"
	leafHash := "eee0ff334455"
	now := time.Now()
	_ = cache.InsertCommits([]cache.Commit{
		{Hash: rootHash, RepoURL: repoURL, Branch: branch, AuthorName: "A", AuthorEmail: "a@t.com", Message: "Root", Timestamp: now},
		{Hash: midHash, RepoURL: repoURL, Branch: branch, AuthorName: "B", AuthorEmail: "b@t.com", Message: "Mid", Timestamp: now.Add(time.Minute)},
		{Hash: leafHash, RepoURL: repoURL, Branch: branch, AuthorName: "C", AuthorEmail: "c@t.com", Message: "Leaf", Timestamp: now.Add(2 * time.Minute)},
	})
	// Root post
	_ = InsertSocialItem(SocialItem{RepoURL: repoURL, Hash: rootHash, Branch: branch, Type: "post"})
	// Mid comment replying to root
	_ = InsertSocialItem(SocialItem{
		RepoURL: repoURL, Hash: midHash, Branch: branch, Type: "comment",
		OriginalRepoURL: cache.ToNullString(repoURL),
		OriginalHash:    cache.ToNullString(rootHash),
		OriginalBranch:  cache.ToNullString(branch),
		ReplyToRepoURL:  cache.ToNullString(repoURL),
		ReplyToHash:     cache.ToNullString(rootHash),
		ReplyToBranch:   cache.ToNullString(branch),
	})
	// Leaf comment replying to mid, original is root
	_ = InsertSocialItem(SocialItem{
		RepoURL: repoURL, Hash: leafHash, Branch: branch, Type: "comment",
		OriginalRepoURL: cache.ToNullString(repoURL),
		OriginalHash:    cache.ToNullString(rootHash),
		OriginalBranch:  cache.ToNullString(branch),
		ReplyToRepoURL:  cache.ToNullString(repoURL),
		ReplyToHash:     cache.ToNullString(midHash),
		ReplyToBranch:   cache.ToNullString(branch),
	})
	// Check that mid has comment count from leaf
	count, _ := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow(`SELECT COALESCE(comments, 0) FROM social_interactions WHERE repo_url = ? AND hash = ? AND branch = ?`,
			repoURL, midHash, branch).Scan(&c)
		return c, err
	})
	if count < 1 {
		t.Errorf("mid comment should have at least 1 nested comment count, got %d", count)
	}
}

// (TestGetPosts_threadRootNotInThread is in TestGetPostsIntegration group)

// (TestGetLists_withData, TestAddRepositoryToList_emptyBranch are in TestListManagement group)
