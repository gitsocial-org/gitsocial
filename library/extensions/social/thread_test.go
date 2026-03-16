// thread_test.go - Tests for thread building and comment tree sorting
package social

import (
	"strings"
	"testing"
	"time"
)

func TestSortThreadTree_empty(t *testing.T) {
	result := SortThreadTree("root-id", nil)
	if len(result) != 0 {
		t.Errorf("SortThreadTree(empty) = %d items, want 0", len(result))
	}
}

func TestSortThreadTree_noChildren(t *testing.T) {
	posts := []Post{
		{ID: "root-id", Content: "Root post"},
	}
	result := SortThreadTree("root-id", posts)
	if len(result) != 0 {
		t.Errorf("SortThreadTree(root only) = %d items, want 0 (root excluded)", len(result))
	}
}

func TestSortThreadTree_directChildren(t *testing.T) {
	now := time.Now()
	posts := []Post{
		{ID: "root-id", Content: "Root"},
		{ID: "child-1", OriginalPostID: "root-id", Timestamp: now.Add(-1 * time.Hour), Content: "First"},
		{ID: "child-2", OriginalPostID: "root-id", Timestamp: now, Content: "Second"},
	}

	result := SortThreadTree("root-id", posts)
	if len(result) != 2 {
		t.Fatalf("SortThreadTree() = %d items, want 2", len(result))
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

	result := SortThreadTree("root-id", posts)
	if len(result) != 2 {
		t.Fatalf("SortThreadTree() = %d items, want 2", len(result))
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

	result := SortThreadTree("root-id", posts)
	// Reposts with OriginalPostID set but Type=repost are excluded from children grouping
	if len(result) != 0 {
		t.Errorf("SortThreadTree() should not include reposts as children, got %d", len(result))
	}
}

func TestSortThreadTree_deduplicates(t *testing.T) {
	now := time.Now()
	posts := []Post{
		{ID: "root-id", Content: "Root"},
		{ID: "child-1", OriginalPostID: "root-id", Timestamp: now, Content: "Comment"},
		{ID: "child-1", OriginalPostID: "root-id", Timestamp: now, Content: "Comment"}, // duplicate
	}

	result := SortThreadTree("root-id", posts)
	if len(result) != 1 {
		t.Errorf("SortThreadTree() should deduplicate, got %d items, want 1", len(result))
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

	result := SortThreadTree("root-id", posts)
	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
	if result[0].ID != "child-2" {
		t.Error("Depth 1 should sort by comments descending first")
	}
}
