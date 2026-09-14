// thread_test.go - Tests for comment reading, thread building and comment tree sorting
package social

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
)

// queryPlan returns the EXPLAIN QUERY PLAN lines SQLite reports for a query.
func queryPlan(t *testing.T, query string, args ...interface{}) []string {
	t.Helper()
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
		t.Fatalf("EXPLAIN QUERY PLAN error = %v", err)
	}
	return lines
}

// TestGetComments_plan pins invariant 3: the comment reader seeks idx_social_original.
func TestGetComments_plan(t *testing.T) {
	setupTestDB(t)
	cases := []struct {
		name   string
		branch string
		args   []interface{}
	}{
		{"with branch", itemsTestBranch, []interface{}{"", itemsTestRepoURL, "plan00000001", itemsTestBranch}},
		{"without branch", "", []interface{}{"", itemsTestRepoURL, "plan00000001"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := strings.Join(queryPlan(t, commentsQuery(tc.branch), tc.args...), "\n")
			if !strings.Contains(plan, "idx_social_original") {
				t.Errorf("plan does not use idx_social_original:\n%s", plan)
			}
			if strings.Contains(plan, "SCAN core_commits") {
				t.Errorf("plan scans core_commits:\n%s", plan)
			}
		})
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
			args := []interface{}{itemsTestRepoURL, "plan00000001", itemsTestBranch, "plan00000001", itemsTestBranch}
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

	posts, err := GetComments(post.Data.Repository, post.Data.Display.CommitHash, post.Data.Branch, post.Data.ID)
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
	posts, err = GetComments(post.Data.Repository, post.Data.Display.CommitHash, post.Data.Branch, post.Data.ID)
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

// TestGetComments_withoutBranch reads a comment in another repository whose original names no branch.
func TestGetComments_withoutBranch(t *testing.T) {
	setupTestDB(t)
	origRepo := "https://github.com/orig/filter"
	origHash := "onb_root1234"
	insertItemsTestCommit(t, origRepo, origHash)
	InsertSocialItem(SocialItem{RepoURL: origRepo, Hash: origHash, Branch: itemsTestBranch, Type: "post"})
	insertItemsTestCommit(t, itemsTestRepoURL, "onb_cmnt1234")
	InsertSocialItem(SocialItem{
		RepoURL: itemsTestRepoURL, Hash: "onb_cmnt1234", Branch: itemsTestBranch, Type: "comment",
		OriginalRepoURL: cache.ToNullString(origRepo),
		OriginalHash:    cache.ToNullString(origHash),
	})

	rootRef := origRepo + "#commit:" + origHash
	posts, err := GetComments(origRepo, origHash, "", rootRef)
	if err != nil {
		t.Fatalf("GetComments() error = %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("GetComments() = %d posts, want 1", len(posts))
	}
	if posts, err = GetComments(origRepo, origHash, itemsTestBranch, rootRef); err != nil {
		t.Fatalf("GetComments(branch) error = %v", err)
	} else if len(posts) != 0 {
		t.Errorf("GetComments(branch) = %d posts, want 0", len(posts))
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
	// ParseRef returns {Type: unknown, Value: "some-plain-id"}, so normalizedKey returns "|some-plain-id|"
	if key != "|some-plain-id|" {
		t.Errorf("normalizedKey(plain) = %q, want %q", key, "|some-plain-id|")
	}
}

func TestNormalizedKey_refWithHash(t *testing.T) {
	key := normalizedKey("#commit:abc123def456@gitmsg/social")
	// ParseRef extracts hash (truncated to 12 chars) and branch
	want := "|abc123def456|gitmsg/social"
	if key != want {
		t.Errorf("normalizedKey(local ref) = %q, want %q", key, want)
	}
}

func TestNormalizedKey_fullRef(t *testing.T) {
	key := normalizedKey("https://github.com/user/repo#commit:abc123def456@gitmsg/social")
	// ParseRef extracts normalized repo, hash, branch
	if !strings.Contains(key, "abc123def456") {
		t.Errorf("normalizedKey(full ref) should contain hash, got %q", key)
	}
	if !strings.Contains(key, "gitmsg/social") {
		t.Errorf("normalizedKey(full ref) should contain branch, got %q", key)
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
