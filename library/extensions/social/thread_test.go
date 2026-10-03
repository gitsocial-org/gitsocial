// thread_test.go - Tests for comment reading, thread building and comment tree sorting
package social

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// queryPlan returns the EXPLAIN QUERY PLAN lines SQLite reports for a query.
func queryPlan(tb testing.TB, query string, args ...interface{}) []string {
	tb.Helper()
	lines, err := cache.QueryLocked(func(db *sql.DB) ([]string, error) {
		rows, err := db.Query("EXPLAIN QUERY PLAN "+query, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var id, parent, notused int
			var detail string
			if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
				return nil, err
			}
			out = append(out, detail)
		}
		return out, rows.Err()
	})
	if err != nil {
		tb.Fatalf("EXPLAIN QUERY PLAN error = %v", err)
	}
	return lines
}

// TestGetComments_plan pins invariant 3: the comment reader seeks idx_social_original.
func TestGetComments_plan(t *testing.T) {
	setupTestDB(t)
	plan := strings.Join(queryPlan(t, commentsQuery, "", itemsTestRepoURL, "plan00000001"), "\n")
	if !strings.Contains(plan, "idx_social_original") {
		t.Errorf("plan does not use idx_social_original:\n%s", plan)
	}
	if strings.Contains(plan, "SCAN core_commits") {
		t.Errorf("plan scans core_commits:\n%s", plan)
	}
}

// threadPlanIndexes are the seeks the thread reader relies on: the reply walk, the originals, the commit rows.
var threadPlanIndexes = []string{"idx_social_reply_to", "idx_social_original", "sqlite_autoindex_core_commits_1"}

// TestGetThread_plan pins the thread reader's seeks and its freedom from a core_commits scan.
func TestGetThread_plan(t *testing.T) {
	setupTestDB(t)
	cases := []struct {
		name     string
		forkURLs []string
	}{
		{"root only", nil},
		{"widened to forks", []string{"https://github.com/fork/one", "https://github.com/fork/two"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			matchURLs := uniqueURLs(itemsTestRepoURL, tc.forkURLs)
			args := []interface{}{itemsTestRepoURL, "plan00000001", itemsTestBranch, "plan00000001"}
			for _, u := range matchURLs {
				args = append(args, u)
			}
			args = append(args, "")
			plan := strings.Join(queryPlan(t, threadQuery(len(matchURLs)), args...), "\n")
			for _, index := range threadPlanIndexes {
				if !strings.Contains(plan, index) {
					t.Errorf("plan does not use %s:\n%s", index, plan)
				}
			}
			if strings.Contains(plan, "SCAN core_commits") {
				t.Errorf("plan scans core_commits:\n%s", plan)
			}
		})
	}
}

// TestGetComments_writePath reads a thread written through CreatePost, CreateComment and RetractPost.
func TestGetComments_writePath(t *testing.T) {
	workdir := initWorkspace(t)
	post := CreatePost(workdir, "Root post", nil)
	if !post.Success {
		t.Fatalf("CreatePost() failed: %s", post.Error.Message)
	}
	comment := CreateComment(workdir, post.Data.ID, "First", nil)
	if !comment.Success {
		t.Fatalf("CreateComment() failed: %s", comment.Error.Message)
	}
	nested := CreateComment(workdir, comment.Data.ID, "Nested", nil)
	if !nested.Success {
		t.Fatalf("CreateComment(nested) failed: %s", nested.Error.Message)
	}

	posts, err := GetComments(post.Data.Repository, post.Data.Display.CommitHash, post.Data.ID)
	if err != nil {
		t.Fatalf("GetComments() error = %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("GetComments() = %d posts, want 2", len(posts))
	}
	if posts[0].Content != "First" || posts[0].Depth != 1 {
		t.Errorf("first post = %q at depth %d, want \"First\" at depth 1", posts[0].Content, posts[0].Depth)
	}
	if posts[1].Content != "Nested" || posts[1].Depth != 2 {
		t.Errorf("second post = %q at depth %d, want \"Nested\" at depth 2", posts[1].Content, posts[1].Depth)
	}

	if r := RetractPost(workdir, nested.Data.ID); !r.Success {
		t.Fatalf("RetractPost(nested) failed: %s", r.Error.Message)
	}
	posts, err = GetComments(post.Data.Repository, post.Data.Display.CommitHash, post.Data.ID)
	if err != nil {
		t.Fatalf("GetComments() after retraction error = %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("GetComments() after retraction = %d posts, want 1", len(posts))
	}
	if posts[0].Content != "First" {
		t.Errorf("remaining post = %q, want \"First\"", posts[0].Content)
	}
}

// TestGetComments_withoutBranch reads a comment in another repository whose original names no branch, under a root that names one.
func TestGetComments_withoutBranch(t *testing.T) {
	setupTestDB(t)
	origRepo := "https://github.com/orig/filter"
	origHash := "0bb000000001"
	insertItemsTestCommit(t, origRepo, origHash)
	InsertSocialItem(SocialItem{RepoURL: origRepo, Hash: origHash, Branch: itemsTestBranch, Type: "post"})
	insertItemsTestCommit(t, itemsTestRepoURL, "0bb000000002")
	InsertSocialItem(SocialItem{
		RepoURL: itemsTestRepoURL, Hash: "0bb000000002", Branch: itemsTestBranch, Type: "comment",
		OriginalRepoURL: cache.ToNullString(origRepo),
		OriginalHash:    cache.ToNullString(origHash),
	})

	for _, rootRef := range []string{origRepo + "#commit:" + origHash, origRepo + "#commit:" + origHash + "@" + itemsTestBranch} {
		posts, err := GetComments(origRepo, origHash, rootRef)
		if err != nil {
			t.Fatalf("GetComments(%s) error = %v", rootRef, err)
		}
		if len(posts) != 1 {
			t.Errorf("GetComments(%s) = %d posts, want 1", rootRef, len(posts))
		}
	}
}

// movedFromBranch is the branch a moved commit had before its home moved to itemsTestBranch.
const movedFromBranch = "feature/x"

// insertMovedCommit caches a commit under movedFromBranch, stale, and under itemsTestBranch, live.
func insertMovedCommit(t *testing.T, repoURL, hash string) {
	t.Helper()
	for _, branch := range []string{movedFromBranch, itemsTestBranch} {
		if err := cache.InsertCommits([]cache.Commit{{
			Hash: hash, RepoURL: repoURL, Branch: branch,
			AuthorName: "Coder", AuthorEmail: "coder@test.com", Message: "Code change",
			Timestamp: time.Date(2025, 10, 20, 12, 0, 0, 0, time.UTC),
		}}); err != nil {
			t.Fatalf("InsertCommits(%s@%s) error = %v", hash, branch, err)
		}
	}
	if err := cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`UPDATE core_commits SET stale_since = '2025-10-21T00:00:00Z' WHERE repo_url = ? AND hash = ? AND branch = ?`,
			repoURL, hash, movedFromBranch)
		return err
	}); err != nil {
		t.Fatalf("mark %s@%s stale: %v", hash, movedFromBranch, err)
	}
}

// insertCommentOn caches a comment whose original names the target on originalBranch.
func insertCommentOn(t *testing.T, hash, targetRepoURL, targetHash, originalBranch string) {
	t.Helper()
	insertItemsTestCommit(t, itemsTestRepoURL, hash)
	if err := InsertSocialItem(SocialItem{
		RepoURL: itemsTestRepoURL, Hash: hash, Branch: itemsTestBranch, Type: "comment",
		OriginalRepoURL: cache.ToNullString(targetRepoURL),
		OriginalHash:    cache.ToNullString(targetHash),
		OriginalBranch:  cache.ToNullString(originalBranch),
	}); err != nil {
		t.Fatalf("InsertSocialItem(%s) error = %v", hash, err)
	}
}

// TestGetComments_movedCommit pins invariant 1: each branch value of the reference lists under the live row.
func TestGetComments_movedCommit(t *testing.T) {
	setupTestDB(t)
	repo := "https://github.com/moved/comments"
	insertMovedCommit(t, repo, "c0de00000001")
	insertCommentOn(t, "c0de00000002", repo, "c0de00000001", movedFromBranch)
	insertCommentOn(t, "c0de00000003", repo, "c0de00000001", itemsTestBranch)

	liveRoot := protocol.CreateRef(protocol.RefTypeCommit, "c0de00000001", repo, itemsTestBranch)
	posts, err := GetComments(repo, "c0de00000001", liveRoot)
	if err != nil {
		t.Fatalf("GetComments() error = %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("GetComments() = %d posts, want both comments", len(posts))
	}
	for _, p := range posts {
		if p.Depth != 1 {
			t.Errorf("comment %s depth = %d, want 1 under the live root", p.ID, p.Depth)
		}
	}
}

// TestGetComments_sameHashTwoRepos pins invariant 15: one hash in two repositories is two targets.
func TestGetComments_sameHashTwoRepos(t *testing.T) {
	setupTestDB(t)
	repoA := "https://github.com/same/a"
	repoB := "https://github.com/same/b"
	insertItemsTestCommit(t, repoA, "5a3e00000001")
	insertItemsTestCommit(t, repoB, "5a3e00000001")
	insertCommentOn(t, "5a3e00000002", repoA, "5a3e00000001", itemsTestBranch)
	insertCommentOn(t, "5a3e00000003", repoB, "5a3e00000001", itemsTestBranch)

	posts, err := GetComments(repoA, "5a3e00000001", protocol.CreateRef(protocol.RefTypeCommit, "5a3e00000001", repoA, itemsTestBranch))
	if err != nil {
		t.Fatalf("GetComments() error = %v", err)
	}
	if len(posts) != 1 || posts[0].Display.CommitHash != "5a3e00000002" {
		t.Errorf("GetComments(repo a) = %+v, want only the comment on repo a", posts)
	}
}

// TestGetThread_movedRoot pins invariant 2: the old and the new branch open one thread with the live root.
func TestGetThread_movedRoot(t *testing.T) {
	workdir := initWorkspace(t)
	repo := "https://github.com/moved/thread"
	insertMovedCommit(t, repo, "7d0000000001")
	insertCommentOn(t, "7d0000000002", repo, "7d0000000001", movedFromBranch)
	insertCommentOn(t, "7d0000000003", repo, "7d0000000001", itemsTestBranch)

	threads := make([][]Post, 0, 2)
	for _, branch := range []string{movedFromBranch, itemsTestBranch} {
		res := getThreadPosts(workdir, protocol.CreateRef(protocol.RefTypeCommit, "7d0000000001", repo, branch), "")
		if !res.Success {
			t.Fatalf("getThreadPosts(@%s) failed: %s", branch, res.Error.Message)
		}
		threads = append(threads, res.Data)
	}
	for i, thread := range threads {
		if len(thread) != 3 {
			t.Fatalf("thread %d = %d posts, want the root and both comments", i, len(thread))
		}
		if thread[0].Branch != itemsTestBranch || thread[0].IsStale {
			t.Errorf("thread %d root = %s stale=%v, want the live row", i, thread[0].ID, thread[0].IsStale)
		}
		for j := range thread {
			if thread[j].ID != threads[0][j].ID {
				t.Errorf("thread %d post %d = %s, want %s", i, j, thread[j].ID, threads[0][j].ID)
			}
		}
	}
}

// TestGetParentChain_movedOriginal pins invariant 4: the chain ends at the live row of the original.
func TestGetParentChain_movedOriginal(t *testing.T) {
	setupTestDB(t)
	repo := "https://github.com/moved/chain"
	insertMovedCommit(t, repo, "9a0000000001")
	insertCommentOn(t, "9a0000000002", repo, "9a0000000001", movedFromBranch)

	parents, err := getParentChain(itemsTestRepoURL, "9a0000000002", itemsTestBranch, "")
	if err != nil {
		t.Fatalf("getParentChain() error = %v", err)
	}
	if len(parents) != 1 {
		t.Fatalf("getParentChain() = %d parents, want 1", len(parents))
	}
	if parents[0].Branch != itemsTestBranch || parents[0].IsStale {
		t.Errorf("parent = %s@%s stale=%v, want the live row", parents[0].Hash, parents[0].Branch, parents[0].IsStale)
	}
}

// TestCreateComment_namesLiveHome pins invariant 13: a comment through an old reference names the live home.
func TestCreateComment_namesLiveHome(t *testing.T) {
	workdir := initWorkspace(t)
	repoURL := gitmsg.ResolveRepoURL(workdir)
	insertMovedCommit(t, repoURL, "e1e000000001")

	res := CreateComment(workdir, protocol.CreateRef(protocol.RefTypeCommit, "e1e000000001", repoURL, movedFromBranch), "Nice change", nil)
	if !res.Success {
		t.Fatalf("CreateComment() failed: %s", res.Error.Message)
	}
	if got := protocol.ParseRef(res.Data.OriginalPostID).Branch; got != itemsTestBranch {
		t.Errorf("original branch = %q, want the live home %q", got, itemsTestBranch)
	}
}

func TestSortThreadTree_empty(t *testing.T) {
	result := sortThreadTree("root-id", nil)
	if len(result) != 0 {
		t.Errorf("sortThreadTree(empty) = %d items, want 0", len(result))
	}
}

func TestSortThreadTree_noChildren(t *testing.T) {
	posts := []Post{
		{ID: "root-id", Content: "Root post"},
	}
	result := sortThreadTree("root-id", posts)
	if len(result) != 0 {
		t.Errorf("sortThreadTree(root only) = %d items, want 0 (root excluded)", len(result))
	}
}

func TestSortThreadTree_directChildren(t *testing.T) {
	now := time.Now()
	posts := []Post{
		{ID: "root-id", Content: "Root"},
		{ID: "child-1", OriginalPostID: "root-id", Timestamp: now.Add(-1 * time.Hour), Content: "First"},
		{ID: "child-2", OriginalPostID: "root-id", Timestamp: now, Content: "Second"},
	}

	result := sortThreadTree("root-id", posts)
	if len(result) != 2 {
		t.Fatalf("sortThreadTree() = %d items, want 2", len(result))
	}
	// Depth 1 children are sorted by comments (desc), then timestamp (asc)
	for _, r := range result {
		if r.Depth != 1 {
			t.Errorf("Direct children should have depth 1, got %d", r.Depth)
		}
	}
}

func TestSortThreadTree_nestedChildren(t *testing.T) {
	now := time.Now()
	posts := []Post{
		{ID: "root-id", Content: "Root"},
		{ID: "child-1", OriginalPostID: "root-id", Timestamp: now.Add(-1 * time.Hour), Content: "Comment"},
		{ID: "grandchild-1", ParentCommentID: "child-1", Timestamp: now, Content: "Reply"},
	}

	result := sortThreadTree("root-id", posts)
	if len(result) != 2 {
		t.Fatalf("sortThreadTree() = %d items, want 2", len(result))
	}
	if result[0].Depth != 1 {
		t.Errorf("First item depth = %d, want 1", result[0].Depth)
	}
	if result[1].Depth != 2 {
		t.Errorf("Second item depth = %d, want 2", result[1].Depth)
	}
}

func TestSortThreadTree_reposts_not_grouped(t *testing.T) {
	now := time.Now()
	posts := []Post{
		{ID: "root-id", Content: "Root"},
		{ID: "repost-1", OriginalPostID: "root-id", Type: PostTypeRepost, Timestamp: now, Content: ""},
	}

	result := sortThreadTree("root-id", posts)
	// Reposts with OriginalPostID set but Type=repost are excluded from children grouping
	if len(result) != 0 {
		t.Errorf("sortThreadTree() should not include reposts as children, got %d", len(result))
	}
}

func TestSortThreadTree_deduplicates(t *testing.T) {
	now := time.Now()
	posts := []Post{
		{ID: "root-id", Content: "Root"},
		{ID: "child-1", OriginalPostID: "root-id", Timestamp: now, Content: "Comment"},
		{ID: "child-1", OriginalPostID: "root-id", Timestamp: now, Content: "Comment"}, // duplicate
	}

	result := sortThreadTree("root-id", posts)
	if len(result) != 1 {
		t.Errorf("sortThreadTree() should deduplicate, got %d items, want 1", len(result))
	}
}

func TestNormalizedKey_plainID(t *testing.T) {
	key := normalizedKey("some-plain-id")
	// ParseRef returns {Type: unknown, Value: "some-plain-id"}, so normalizedKey returns "|some-plain-id"
	if key != "|some-plain-id" {
		t.Errorf("normalizedKey(plain) = %q, want %q", key, "|some-plain-id")
	}
}

func TestNormalizedKey_refWithHash(t *testing.T) {
	key := normalizedKey("#commit:abc123def456@gitmsg/social")
	// ParseRef extracts the hash, truncated to 12 chars; the key drops the branch
	want := "|abc123def456"
	if key != want {
		t.Errorf("normalizedKey(local ref) = %q, want %q", key, want)
	}
}

func TestNormalizedKey_fullRef(t *testing.T) {
	key := normalizedKey("https://github.com/user/repo#commit:abc123def456@gitmsg/social")
	if want := "https://github.com/user/repo|abc123def456"; key != want {
		t.Errorf("normalizedKey(full ref) = %q, want %q", key, want)
	}
}

func TestNormalizedKey_samePostDifferentFormat(t *testing.T) {
	// Two refs pointing to the same commit should produce the same key
	local := normalizedKey("#commit:abc123def456@gitmsg/social")
	full := normalizedKey("https://github.com/user/repo#commit:abc123def456@gitmsg/social")
	// They differ because full ref includes the repository
	if local == full {
		t.Error("local and full refs should differ since they have different repo context")
	}
	if other := normalizedKey("https://github.com/user/repo#commit:abc123def456@main"); other != full {
		t.Errorf("normalizedKey(@main) = %q, want %q: two branch values name one post", other, full)
	}
}

// TestSortThreadTree_branchAgnostic pins invariant 3: a comment attaches under its root whatever branch its reference names.
func TestSortThreadTree_branchAgnostic(t *testing.T) {
	now := time.Now()
	repo := "https://github.com/user/repo"
	root := repo + "#commit:aaaa00000001@main"
	child := repo + "#commit:aaaa00000002@gitmsg/social"
	posts := []Post{
		{ID: child, OriginalPostID: repo + "#commit:aaaa00000001@feature/x", Timestamp: now.Add(-time.Hour)},
		{ID: repo + "#commit:aaaa00000002@main", OriginalPostID: repo + "#commit:aaaa00000001@feature/x", Timestamp: now.Add(-time.Hour), IsStale: true},
		{ID: repo + "#commit:aaaa00000003@gitmsg/social", OriginalPostID: root, ParentCommentID: repo + "#commit:aaaa00000002", Timestamp: now},
	}
	result := sortThreadTree(root, posts)
	if len(result) != 2 {
		t.Fatalf("sortThreadTree() = %d posts, want the comment once and its reply", len(result))
	}
	if result[0].ID != child || result[0].Depth != 1 {
		t.Errorf("first = %s at depth %d, want the live %s at depth 1", result[0].ID, result[0].Depth, child)
	}
	if result[1].Depth != 2 {
		t.Errorf("reply depth = %d, want 2", result[1].Depth)
	}
}

func TestNormalizedKey_empty(t *testing.T) {
	key := normalizedKey("")
	// Empty string can't be parsed, returned as-is
	if key != "" {
		t.Errorf("normalizedKey('') = %q, want empty", key)
	}
}

func TestSortThreadTree_depth1SortByComments(t *testing.T) {
	now := time.Now()
	posts := []Post{
		{ID: "root-id", Content: "Root"},
		{ID: "child-1", OriginalPostID: "root-id", Timestamp: now.Add(-1 * time.Hour), Content: "Less popular", Interactions: Interactions{Comments: 1}},
		{ID: "child-2", OriginalPostID: "root-id", Timestamp: now, Content: "Popular", Interactions: Interactions{Comments: 10}},
	}

	result := sortThreadTree("root-id", posts)
	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
	if result[0].ID != "child-2" {
		t.Error("Depth 1 should sort by comments descending first")
	}
}
