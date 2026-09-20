// items_test.go - Tests for social item queries, conversions, and cache operations
package social

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/protocol"
)

// --- Pure function tests ---

func TestCreateVirtualSocialItem_wrongExt(t *testing.T) {
	ref := protocol.Ref{Ext: "pm", Metadata: "> Hello", Ref: "#commit:abc123@main"}
	if got := createVirtualSocialItem(ref, "https://github.com/a/b", "main"); got != nil {
		t.Error("wrong ext should return nil")
	}
}

func TestCreateVirtualSocialItem_noMetadata(t *testing.T) {
	ref := protocol.Ref{Ext: "social", Metadata: "", Ref: "#commit:abc123@main"}
	if got := createVirtualSocialItem(ref, "https://github.com/a/b", "main"); got != nil {
		t.Error("empty metadata should return nil")
	}
}

func TestCreateVirtualSocialItem_noContent(t *testing.T) {
	ref := protocol.Ref{Ext: "social", Metadata: "not a quote line", Ref: "#commit:abc123@main"}
	if got := createVirtualSocialItem(ref, "https://github.com/a/b", "main"); got != nil {
		t.Error("no quoted content should return nil")
	}
}

func TestCreateVirtualSocialItem_noTimestamp(t *testing.T) {
	ref := protocol.Ref{
		Ext:      "social",
		Metadata: "> Hello",
		Ref:      "#commit:abc123@main",
		Time:     "",
	}
	if got := createVirtualSocialItem(ref, "https://github.com/a/b", "main"); got != nil {
		t.Error("invalid timestamp should return nil")
	}
}

func TestCreateVirtualSocialItem_happyPath(t *testing.T) {
	ts := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	ref := protocol.Ref{
		Ext:      "social",
		Author:   "Alice",
		Email:    "alice@test.com",
		Time:     ts.Format(time.RFC3339),
		Ref:      "https://github.com/a/b#commit:abc123def456@main",
		V:        "0.1.0",
		Fields:   map[string]string{"type": "post"},
		Metadata: "> Hello world\n> Second line",
	}
	got := createVirtualSocialItem(ref, "https://github.com/fallback/repo", "develop")
	if got == nil {
		t.Fatal("should return non-nil item")
	}
	if got.RepoURL != "https://github.com/a/b" {
		t.Errorf("RepoURL = %q", got.RepoURL)
	}
	if got.Hash != "abc123def456" {
		t.Errorf("Hash = %q", got.Hash)
	}
	if got.Branch != "main" {
		t.Errorf("Branch = %q, want main", got.Branch)
	}
	if got.Type != "post" {
		t.Errorf("Type = %q", got.Type)
	}
	if got.Content != "Hello world\nSecond line" {
		t.Errorf("Content = %q", got.Content)
	}
	if got.AuthorName != "Alice" {
		t.Errorf("AuthorName = %q", got.AuthorName)
	}
	if !got.IsVirtual {
		t.Error("should be virtual")
	}
}

func TestCreateVirtualSocialItem_fallbackBranch(t *testing.T) {
	ts := time.Now().Format(time.RFC3339)
	ref := protocol.Ref{
		Ext:      "social",
		Time:     ts,
		Ref:      "#commit:abc123def456",
		Metadata: "> content",
	}
	got := createVirtualSocialItem(ref, "https://github.com/a/b", "develop")
	if got == nil {
		t.Fatal("should return non-nil")
	}
	if got.Branch != "develop" {
		t.Errorf("Branch = %q, want develop (fallback)", got.Branch)
	}
	if got.RepoURL != "https://github.com/a/b" {
		t.Errorf("RepoURL = %q, want fallback", got.RepoURL)
	}
}

func TestCreateVirtualSocialItem_defaultType(t *testing.T) {
	ts := time.Now().Format(time.RFC3339)
	ref := protocol.Ref{
		Ext:      "social",
		Time:     ts,
		Ref:      "#commit:abc123def456@main",
		Metadata: "> content",
		Fields:   map[string]string{},
	}
	got := createVirtualSocialItem(ref, "https://github.com/a/b", "main")
	if got == nil {
		t.Fatal("should return non-nil")
	}
	if got.Type != "post" {
		t.Errorf("Type = %q, want post (default)", got.Type)
	}
}

func TestSocialItemToPost_basic(t *testing.T) {
	item := SocialItem{
		RepoURL:     "https://github.com/a/b",
		Hash:        "abc123def456",
		Branch:      "main",
		Type:        "post",
		Content:     "Hello world",
		AuthorName:  "Alice",
		AuthorEmail: "alice@test.com",
		Timestamp:   time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC),
		Comments:    5,
		Reposts:     3,
		Quotes:      1,
	}

	post := SocialItemToPost(item)
	if post.Repository != "https://github.com/a/b" {
		t.Errorf("Repository = %q", post.Repository)
	}
	if post.Branch != "main" {
		t.Errorf("Branch = %q", post.Branch)
	}
	if post.Type != PostTypePost {
		t.Errorf("Type = %q", post.Type)
	}
	if post.Content != "Hello world" {
		t.Errorf("Content = %q", post.Content)
	}
	if post.Author.Name != "Alice" {
		t.Errorf("Author.Name = %q", post.Author.Name)
	}
	if post.Interactions.Comments != 5 {
		t.Errorf("Comments = %d", post.Interactions.Comments)
	}
	if post.Display.TotalReposts != 4 {
		t.Errorf("TotalReposts = %d, want 4 (reposts+quotes)", post.Display.TotalReposts)
	}
}

func TestSocialItemToPost_emptyType(t *testing.T) {
	item := SocialItem{Hash: "abc123", Branch: "main"}
	post := SocialItemToPost(item)
	if post.Type != PostTypePost {
		t.Errorf("empty type should default to post, got %q", post.Type)
	}
}

func TestSocialItemToPost_emptyRepoURL(t *testing.T) {
	item := SocialItem{Hash: "abc123", Branch: "main", Type: "post"}
	post := SocialItemToPost(item)
	if post.Repository != "" {
		t.Errorf("Repository = %q, want empty for an item with no repo URL", post.Repository)
	}
}

func TestSocialItemToPost_withOriginal(t *testing.T) {
	item := SocialItem{
		RepoURL:         "https://github.com/a/b",
		Hash:            "comment1",
		Branch:          "main",
		Type:            "comment",
		OriginalRepoURL: sql.NullString{String: "https://github.com/c/d", Valid: true},
		OriginalHash:    sql.NullString{String: "post1", Valid: true},
		OriginalBranch:  sql.NullString{String: "main", Valid: true},
	}
	post := SocialItemToPost(item)
	if post.OriginalPostID == "" {
		t.Error("OriginalPostID should be set")
	}
}

func TestSocialItemToPost_withReplyTo(t *testing.T) {
	item := SocialItem{
		RepoURL:        "https://github.com/a/b",
		Hash:           "reply1",
		Branch:         "main",
		Type:           "comment",
		ReplyToRepoURL: sql.NullString{String: "https://github.com/a/b", Valid: true},
		ReplyToHash:    sql.NullString{String: "comment1", Valid: true},
		ReplyToBranch:  sql.NullString{String: "main", Valid: true},
	}
	post := SocialItemToPost(item)
	if post.ParentCommentID == "" {
		t.Error("ParentCommentID should be set")
	}
}

func TestSocialItemToPost_withEditOf(t *testing.T) {
	item := SocialItem{
		Hash:   "edit1",
		Branch: "main",
		Type:   "post",
		EditOf: sql.NullString{String: "#commit:orig123@main", Valid: true},
	}
	post := SocialItemToPost(item)
	if post.EditOf != "#commit:orig123@main" {
		t.Errorf("EditOf = %q", post.EditOf)
	}
}

func TestSocialItemToPost_editOfEmpty(t *testing.T) {
	item := SocialItem{Hash: "abc", Branch: "main", Type: "post", EditOf: sql.NullString{}}
	post := SocialItemToPost(item)
	if post.EditOf != "" {
		t.Errorf("EditOf should be empty, got %q", post.EditOf)
	}
}

func TestSocialItemToPost_crlfStripped(t *testing.T) {
	item := SocialItem{Hash: "abc", Branch: "main", Type: "post", Content: "Hello\r\nworld"}
	post := SocialItemToPost(item)
	if post.Content != "Hello\nworld" {
		t.Errorf("Content = %q, should strip \\r", post.Content)
	}
}

func TestSocialItemToPost_followsWorkspace(t *testing.T) {
	item := SocialItem{Hash: "abc", Branch: "main", Type: "post", FollowsWorkspace: true}
	post := SocialItemToPost(item)
	if !post.Display.FollowsYou {
		t.Error("FollowsYou should be true when FollowsWorkspace is true")
	}
}

func TestExtractOriginalExtType_nilMsg(t *testing.T) {
	ext, typ := extractOriginalExtType("")
	if ext != "" || typ != "" {
		t.Errorf("nil msg: ext=%q typ=%q", ext, typ)
	}
}

func TestExtractOriginalExtType_noRefs(t *testing.T) {
	ext, typ := extractOriginalExtType("just a plain message")
	if ext != "" || typ != "" {
		t.Errorf("no refs: ext=%q typ=%q", ext, typ)
	}
}

func TestExtractOriginalExtType_validRef(t *testing.T) {
	header := protocol.Header{Ext: "social", V: "0.1.0", Fields: map[string]string{"type": "comment"}}
	ref := protocol.Ref{
		Ext:    "pm",
		V:      "0.1.0",
		Ref:    "#commit:abc123@gitmsg/pm",
		Author: "Alice",
		Email:  "alice@test.com",
		Time:   "2025-06-15T12:00:00Z",
		Fields: map[string]string{"type": "issue"},
	}
	msg := protocol.FormatMessage("content", header, []protocol.Ref{ref})
	ext, typ := extractOriginalExtType(msg)
	if ext != "pm" {
		t.Errorf("ext = %q, want pm", ext)
	}
	if typ != "issue" {
		t.Errorf("typ = %q, want issue", typ)
	}
}

func TestExtractHeaderFields_nilMsg(t *testing.T) {
	ext, typ, state := extractHeaderFields("")
	if ext != "" || typ != "" || state != "" {
		t.Errorf("nil msg: ext=%q typ=%q state=%q", ext, typ, state)
	}
}

func TestExtractHeaderFields_validMsg(t *testing.T) {
	msg := "content\n\nGitMsg: ext=\"pm\"; type=\"issue\"; state=\"open\"; v=\"0.1.0\""
	ext, typ, state := extractHeaderFields(msg)
	if ext != "pm" {
		t.Errorf("ext = %q, want pm", ext)
	}
	if typ != "issue" {
		t.Errorf("typ = %q, want issue", typ)
	}
	if state != "open" {
		t.Errorf("state = %q, want open", state)
	}
}

// --- DB tests ---

const itemsTestRepoURL = "https://github.com/test/items"
const itemsTestBranch = "main"

func insertItemsTestCommit(t *testing.T, repoURL, hash string) {
	t.Helper()
	if err := cache.InsertCommits([]cache.Commit{{
		Hash:        hash,
		RepoURL:     repoURL,
		Branch:      itemsTestBranch,
		AuthorName:  "Test User",
		AuthorEmail: "test@test.com",
		Message:     "test commit",
		Timestamp:   time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
}

func TestInsertSocialItem_virtual(t *testing.T) {
	setupTestDB(t)
	item := SocialItem{
		RepoURL:     itemsTestRepoURL,
		Hash:        "virt12345678",
		Branch:      itemsTestBranch,
		Type:        "post",
		Content:     "virtual content",
		AuthorName:  "Virtual",
		AuthorEmail: "v@test.com",
		Timestamp:   time.Now(),
		IsVirtual:   true,
	}
	if err := InsertSocialItem(item); err != nil {
		t.Fatalf("InsertSocialItem(virtual) error = %v", err)
	}
	count := countSocialItems(t)
	if count != 1 {
		t.Errorf("expected 1 social_item, got %d", count)
	}
}

func TestInsertSocialItem_real(t *testing.T) {
	setupTestDB(t)
	insertItemsTestCommit(t, itemsTestRepoURL, "real12345678")
	item := SocialItem{
		RepoURL: itemsTestRepoURL,
		Hash:    "real12345678",
		Branch:  itemsTestBranch,
		Type:    "post",
	}
	if err := InsertSocialItem(item); err != nil {
		t.Fatalf("InsertSocialItem(real) error = %v", err)
	}
	count := countSocialItems(t)
	if count != 1 {
		t.Errorf("expected 1 social_item, got %d", count)
	}
}

func TestInsertSocialItem_upgradeVirtual(t *testing.T) {
	setupTestDB(t)
	// Insert virtual first
	vi := SocialItem{
		RepoURL:     itemsTestRepoURL,
		Hash:        "upgr12345678",
		Branch:      itemsTestBranch,
		Type:        "post",
		Content:     "virtual",
		AuthorName:  "V",
		AuthorEmail: "v@test.com",
		Timestamp:   time.Now(),
		IsVirtual:   true,
	}
	if err := InsertSocialItem(vi); err != nil {
		t.Fatalf("insert virtual: %v", err)
	}

	// Insert real commit
	if err := cache.InsertCommits([]cache.Commit{{
		Hash:        "upgr12345678",
		RepoURL:     itemsTestRepoURL,
		Branch:      itemsTestBranch,
		AuthorName:  "Real User",
		AuthorEmail: "real@test.com",
		Message:     "real content",
		Timestamp:   time.Now(),
	}}); err != nil {
		t.Fatalf("insert real commit: %v", err)
	}

	// Insert real social item (should upgrade virtual)
	ri := SocialItem{
		RepoURL: itemsTestRepoURL,
		Hash:    "upgr12345678",
		Branch:  itemsTestBranch,
		Type:    "post",
	}
	if err := InsertSocialItem(ri); err != nil {
		t.Fatalf("insert real item: %v", err)
	}

	// Verify is_virtual flipped
	isVirtual, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var v int
		err := db.QueryRow(`SELECT is_virtual FROM core_commits WHERE repo_url = ? AND hash = ? AND branch = ?`,
			itemsTestRepoURL, "upgr12345678", itemsTestBranch).Scan(&v)
		return v, err
	})
	if err != nil {
		t.Fatalf("query is_virtual: %v", err)
	}
	if isVirtual != 0 {
		t.Error("is_virtual should be 0 after upgrade")
	}
}

func TestInsertSocialItem_interactionCounts(t *testing.T) {
	setupTestDB(t)
	// Insert a root post
	insertItemsTestCommit(t, itemsTestRepoURL, "root12345678")
	InsertSocialItem(SocialItem{
		RepoURL: itemsTestRepoURL,
		Hash:    "root12345678",
		Branch:  itemsTestBranch,
		Type:    "post",
	})

	// Insert a comment on the root post
	insertItemsTestCommit(t, itemsTestRepoURL, "cmnt12345678")
	InsertSocialItem(SocialItem{
		RepoURL:         itemsTestRepoURL,
		Hash:            "cmnt12345678",
		Branch:          itemsTestBranch,
		Type:            "comment",
		OriginalRepoURL: cache.ToNullString(itemsTestRepoURL),
		OriginalHash:    cache.ToNullString("root12345678"),
		OriginalBranch:  cache.ToNullString(itemsTestBranch),
	})

	// Verify interaction counts
	counts, err := RefreshInteractionCounts(itemsTestRepoURL, "root12345678", itemsTestBranch)
	if err != nil {
		t.Fatalf("RefreshInteractionCounts() error = %v", err)
	}
	if counts.Comments != 1 {
		t.Errorf("Comments = %d, want 1", counts.Comments)
	}
}

func TestGetCachedCommit(t *testing.T) {
	setupTestDB(t)
	msg := "Hello world\n\n" + `GitMsg: ext="social"; type="post"; v="0.1.0"`
	if err := cache.InsertCommits([]cache.Commit{{
		Hash:        "ccmt12345678",
		RepoURL:     itemsTestRepoURL,
		Branch:      itemsTestBranch,
		AuthorName:  "Test User",
		AuthorEmail: "test@test.com",
		Message:     msg,
		Timestamp:   time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}

	item, err := getCachedCommit(itemsTestRepoURL, "ccmt12345678", itemsTestBranch)
	if err != nil {
		t.Fatalf("getCachedCommit() error = %v", err)
	}
	if item.Hash != "ccmt12345678" {
		t.Errorf("Hash = %q", item.Hash)
	}
	if item.Type != "post" {
		t.Errorf("Type = %q, want post", item.Type)
	}
	if item.AuthorName != "Test User" {
		t.Errorf("AuthorName = %q", item.AuthorName)
	}
	if item.HeaderExt != "social" {
		t.Errorf("HeaderExt = %q, want social", item.HeaderExt)
	}
}

func TestGetCachedCommit_notFound(t *testing.T) {
	setupTestDB(t)
	_, err := getCachedCommit("https://github.com/no/repo", "nonexistent", "main")
	if err == nil {
		t.Error("expected error for missing commit")
	}
}

func TestGetSocialItem(t *testing.T) {
	setupTestDB(t)
	insertItemsTestCommit(t, itemsTestRepoURL, "gsi_12345678")
	InsertSocialItem(SocialItem{
		RepoURL: itemsTestRepoURL,
		Hash:    "gsi_12345678",
		Branch:  itemsTestBranch,
		Type:    "post",
	})

	item, err := GetSocialItem(itemsTestRepoURL, "gsi_12345678", itemsTestBranch, "")
	if err != nil {
		t.Fatalf("GetSocialItem() error = %v", err)
	}
	if item.Hash != "gsi_12345678" {
		t.Errorf("Hash = %q", item.Hash)
	}
}

func TestGetSocialItemByRef(t *testing.T) {
	setupTestDB(t)
	insertItemsTestCommit(t, itemsTestRepoURL, "aef012345678")
	InsertSocialItem(SocialItem{
		RepoURL: itemsTestRepoURL,
		Hash:    "aef012345678",
		Branch:  itemsTestBranch,
		Type:    "post",
	})

	refStr := itemsTestRepoURL + "#commit:aef012345678@" + itemsTestBranch
	item, err := GetSocialItemByRef(refStr, "")
	if err != nil {
		t.Fatalf("GetSocialItemByRef() error = %v", err)
	}
	if item.Hash != "aef012345678" {
		t.Errorf("Hash = %q", item.Hash)
	}
}

func TestGetSocialItemByRef_emptyRef(t *testing.T) {
	setupTestDB(t)
	_, err := GetSocialItemByRef("", "")
	if err == nil {
		t.Error("expected error for empty ref")
	}
}

func TestGetSocialItems_filterByType(t *testing.T) {
	setupTestDB(t)
	insertItemsTestCommit(t, itemsTestRepoURL, "typ1_1234567")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "typ1_1234567", Branch: itemsTestBranch, Type: "post"})
	insertItemsTestCommit(t, itemsTestRepoURL, "typ2_1234567")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "typ2_1234567", Branch: itemsTestBranch, Type: "comment",
		OriginalRepoURL: cache.ToNullString(itemsTestRepoURL), OriginalHash: cache.ToNullString("typ1_1234567"), OriginalBranch: cache.ToNullString(itemsTestBranch)})

	items, err := getSocialItems(socialQuery{Types: []string{"post"}, RepoURL: itemsTestRepoURL})
	if err != nil {
		t.Fatalf("getSocialItems() error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 post, got %d", len(items))
	}
}

func TestGetSocialItems_limitOffset(t *testing.T) {
	setupTestDB(t)
	for i := 0; i < 5; i++ {
		hash := "pag" + string(rune('a'+i)) + "_1234567"
		if err := cache.InsertCommits([]cache.Commit{{
			Hash:        hash,
			RepoURL:     itemsTestRepoURL,
			Branch:      itemsTestBranch,
			AuthorName:  "Test",
			AuthorEmail: "test@test.com",
			Message:     "test",
			Timestamp:   time.Date(2025, 10, 21, 12, i, 0, 0, time.UTC),
		}}); err != nil {
			t.Fatal(err)
		}
		InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: hash, Branch: itemsTestBranch, Type: "post"})
	}

	items, err := getSocialItems(socialQuery{RepoURL: itemsTestRepoURL, Limit: 2})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items with limit=2, got %d", len(items))
	}
}

func TestGetSocialItems_sinceUntil(t *testing.T) {
	setupTestDB(t)
	if err := cache.InsertCommits([]cache.Commit{{
		Hash:        "date12345678",
		RepoURL:     itemsTestRepoURL,
		Branch:      itemsTestBranch,
		AuthorName:  "Test",
		AuthorEmail: "test@test.com",
		Message:     "test",
		Timestamp:   time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "date12345678", Branch: itemsTestBranch, Type: "post"})

	since := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2025, 6, 30, 0, 0, 0, 0, time.UTC)
	items, err := getSocialItems(socialQuery{RepoURL: itemsTestRepoURL, Since: &since, Until: &until})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item in range, got %d", len(items))
	}

	before := time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)
	items2, err := getSocialItems(socialQuery{RepoURL: itemsTestRepoURL, Until: &before})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items2) != 0 {
		t.Errorf("expected 0 items before May, got %d", len(items2))
	}
}

func TestGetTimeline(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/ws/repo"
	insertItemsTestCommit(t, wsURL, "tl_112345678")
	InsertSocialItem(SocialItem{RepoURL: wsURL, Hash: "tl_112345678", Branch: itemsTestBranch, Type: "post"})

	items, err := getTimeline(nil, wsURL, wsURL, nil, 10, "")
	if err != nil {
		t.Fatalf("getTimeline() error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 timeline item, got %d", len(items))
	}
}

func TestGetTimeline_empty(t *testing.T) {
	setupTestDB(t)
	items, err := getTimeline(nil, "", "", nil, 10, "")
	if err != nil {
		t.Fatalf("getTimeline(empty) error = %v", err)
	}
	if items != nil {
		t.Errorf("expected nil for empty timeline, got %v", items)
	}
}

func TestGetThread(t *testing.T) {
	setupTestDB(t)
	insertItemsTestCommit(t, itemsTestRepoURL, "thrd_root123")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "thrd_root123", Branch: itemsTestBranch, Type: "post"})

	insertItemsTestCommit(t, itemsTestRepoURL, "thrd_cmnt123")
	InsertSocialItem(SocialItem{
		RepoURL:         itemsTestRepoURL,
		Hash:            "thrd_cmnt123",
		Branch:          itemsTestBranch,
		Type:            "comment",
		OriginalRepoURL: cache.ToNullString(itemsTestRepoURL),
		OriginalHash:    cache.ToNullString("thrd_root123"),
		OriginalBranch:  cache.ToNullString(itemsTestBranch),
		ReplyToRepoURL:  cache.ToNullString(itemsTestRepoURL),
		ReplyToHash:     cache.ToNullString("thrd_root123"),
		ReplyToBranch:   cache.ToNullString(itemsTestBranch),
	})

	items, err := getThread(itemsTestRepoURL, "thrd_root123", itemsTestBranch, "", nil)
	if err != nil {
		t.Fatalf("getThread() error = %v", err)
	}
	if len(items) < 2 {
		t.Errorf("expected at least 2 thread items, got %d", len(items))
	}
}

func TestGetParentChain(t *testing.T) {
	setupTestDB(t)
	// root -> child -> grandchild
	insertItemsTestCommit(t, itemsTestRepoURL, "pc_root12345")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "pc_root12345", Branch: itemsTestBranch, Type: "post"})

	insertItemsTestCommit(t, itemsTestRepoURL, "pc_chld12345")
	InsertSocialItem(SocialItem{
		RepoURL:         itemsTestRepoURL,
		Hash:            "pc_chld12345",
		Branch:          itemsTestBranch,
		Type:            "comment",
		OriginalRepoURL: cache.ToNullString(itemsTestRepoURL),
		OriginalHash:    cache.ToNullString("pc_root12345"),
		OriginalBranch:  cache.ToNullString(itemsTestBranch),
		ReplyToRepoURL:  cache.ToNullString(itemsTestRepoURL),
		ReplyToHash:     cache.ToNullString("pc_root12345"),
		ReplyToBranch:   cache.ToNullString(itemsTestBranch),
	})

	insertItemsTestCommit(t, itemsTestRepoURL, "pc_grch12345")
	InsertSocialItem(SocialItem{
		RepoURL:         itemsTestRepoURL,
		Hash:            "pc_grch12345",
		Branch:          itemsTestBranch,
		Type:            "comment",
		OriginalRepoURL: cache.ToNullString(itemsTestRepoURL),
		OriginalHash:    cache.ToNullString("pc_root12345"),
		OriginalBranch:  cache.ToNullString(itemsTestBranch),
		ReplyToRepoURL:  cache.ToNullString(itemsTestRepoURL),
		ReplyToHash:     cache.ToNullString("pc_chld12345"),
		ReplyToBranch:   cache.ToNullString(itemsTestBranch),
	})

	parents, err := getParentChain(itemsTestRepoURL, "pc_grch12345", itemsTestBranch, "")
	if err != nil {
		t.Fatalf("getParentChain() error = %v", err)
	}
	if len(parents) < 1 {
		t.Errorf("expected at least 1 parent, got %d", len(parents))
	}
}

func TestRefreshInteractionCounts_none(t *testing.T) {
	setupTestDB(t)
	counts, err := RefreshInteractionCounts("https://github.com/no/repo", "nonexistent", "main")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if counts.Comments != 0 || counts.Reposts != 0 || counts.Quotes != 0 {
		t.Errorf("expected zeros, got %+v", counts)
	}
}

func TestRefreshInteractionCounts_withInteractions(t *testing.T) {
	setupTestDB(t)
	insertItemsTestCommit(t, itemsTestRepoURL, "ric_root1234")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "ric_root1234", Branch: itemsTestBranch, Type: "post"})

	// Add comment
	insertItemsTestCommit(t, itemsTestRepoURL, "ric_cmnt1234")
	InsertSocialItem(SocialItem{
		RepoURL: itemsTestRepoURL, Hash: "ric_cmnt1234", Branch: itemsTestBranch, Type: "comment",
		OriginalRepoURL: cache.ToNullString(itemsTestRepoURL), OriginalHash: cache.ToNullString("ric_root1234"), OriginalBranch: cache.ToNullString(itemsTestBranch),
	})

	// Add repost
	insertItemsTestCommit(t, itemsTestRepoURL, "ric_rpst1234")
	InsertSocialItem(SocialItem{
		RepoURL: itemsTestRepoURL, Hash: "ric_rpst1234", Branch: itemsTestBranch, Type: "repost",
		OriginalRepoURL: cache.ToNullString(itemsTestRepoURL), OriginalHash: cache.ToNullString("ric_root1234"), OriginalBranch: cache.ToNullString(itemsTestBranch),
	})

	counts, err := RefreshInteractionCounts(itemsTestRepoURL, "ric_root1234", itemsTestBranch)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if counts.Comments != 1 {
		t.Errorf("Comments = %d, want 1", counts.Comments)
	}
	if counts.Reposts != 1 {
		t.Errorf("Reposts = %d, want 1", counts.Reposts)
	}
}

// commentsFor reads a post's comment count from its ref.
func commentsFor(t *testing.T, postID string) int {
	t.Helper()
	parsed := protocol.ParseRef(postID)
	counts, err := RefreshInteractionCounts(parsed.Repository, parsed.Value, parsed.Branch)
	if err != nil {
		t.Fatalf("RefreshInteractionCounts(%s) error = %v", postID, err)
	}
	return counts.Comments
}

// fetchSocialCommit ingests one commit on the social branch the way the fetch path does.
func fetchSocialCommit(t *testing.T, repoURL, hash, message string) {
	t.Helper()
	const branch = "gitmsg/social"
	now := time.Now()
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: hash, RepoURL: repoURL, Branch: branch,
		AuthorName: "Remote", AuthorEmail: "remote@test.com", Message: message, Timestamp: now,
	}}); err != nil {
		t.Fatalf("InsertCommits(%s) error = %v", hash, err)
	}
	gc := git.Commit{Hash: hash, Message: message, Author: "Remote", Email: "remote@test.com", Timestamp: now}
	processSocialCommit(gc, protocol.ParseMessage(message), repoURL, branch)
}

// TestInteractionCounts_writePath pins invariant 2 on posts written locally.
func TestInteractionCounts_writePath(t *testing.T) {
	workdir := initWorkspace(t)
	post := CreatePost(workdir, "Root post", nil)
	if !post.Success {
		t.Fatalf("CreatePost() failed: %s", post.Error.Message)
	}
	comment := CreateComment(workdir, post.Data.ID, "First", nil)
	if !comment.Success {
		t.Fatalf("CreateComment() failed: %s", comment.Error.Message)
	}
	if got := commentsFor(t, post.Data.ID); got != 1 {
		t.Errorf("root comments after one comment = %d, want 1", got)
	}
	nested := CreateComment(workdir, comment.Data.ID, "Nested", nil)
	if !nested.Success {
		t.Fatalf("CreateComment(nested) failed: %s", nested.Error.Message)
	}
	if got := commentsFor(t, comment.Data.ID); got != 1 {
		t.Errorf("comment comments after one reply = %d, want 1", got)
	}
	if got := commentsFor(t, post.Data.ID); got != 2 {
		t.Errorf("root comments after a nested reply = %d, want 2", got)
	}
	if r := RetractPost(workdir, nested.Data.ID); !r.Success {
		t.Fatalf("RetractPost(nested) failed: %s", r.Error.Message)
	}
	if got := commentsFor(t, comment.Data.ID); got != 0 {
		t.Errorf("comment comments after the reply is retracted = %d, want 0", got)
	}
	if got := commentsFor(t, post.Data.ID); got != 1 {
		t.Errorf("root comments after the reply is retracted = %d, want 1", got)
	}
	if r := RetractPost(workdir, comment.Data.ID); !r.Success {
		t.Fatalf("RetractPost(comment) failed: %s", r.Error.Message)
	}
	if got := commentsFor(t, post.Data.ID); got != 0 {
		t.Errorf("root comments after the comment is retracted = %d, want 0", got)
	}
}

// TestInteractionCounts_fetchedRetraction pins invariant 2 on the fetch path.
func TestInteractionCounts_fetchedRetraction(t *testing.T) {
	setupTestDB(t)
	repo := "https://github.com/counts/fetched"
	branch := "gitmsg/social"
	fetchSocialCommit(t, repo, "a11100000001", "Root post")
	fetchSocialCommit(t, repo, "a11100000002",
		"Nice\n\n"+`GitMsg: ext="social"; type="comment"; original="#commit:a11100000001@gitmsg/social"; v="0.1.0"`)
	counts, err := RefreshInteractionCounts(repo, "a11100000001", branch)
	if err != nil {
		t.Fatalf("RefreshInteractionCounts() error = %v", err)
	}
	if counts.Comments != 1 {
		t.Fatalf("root comments after a fetched comment = %d, want 1", counts.Comments)
	}
	fetchSocialCommit(t, repo, "a11100000003",
		"\n\n"+`GitMsg: ext="social"; type="comment"; edits="#commit:a11100000002@gitmsg/social"; retracted="true"; original="#commit:a11100000001@gitmsg/social"; v="0.1.0"`)
	counts, err = RefreshInteractionCounts(repo, "a11100000001", branch)
	if err != nil {
		t.Fatalf("RefreshInteractionCounts() error = %v", err)
	}
	if counts.Comments != 0 {
		t.Errorf("root comments after a fetched retraction = %d, want 0", counts.Comments)
	}
}

// TestInteractionCounts_forkMirrorCountsOnce pins one count per source hash.
func TestInteractionCounts_forkMirrorCountsOnce(t *testing.T) {
	setupTestDB(t)
	repo := "https://github.com/counts/origin"
	fork := "https://github.com/counts/mirror"
	branch := "gitmsg/social"
	comment := "Nice\n\n" + `GitMsg: ext="social"; type="comment"; original="` + repo + `#commit:b22200000001@gitmsg/social"; v="0.1.0"`
	fetchSocialCommit(t, repo, "b22200000001", "Root post")
	fetchSocialCommit(t, repo, "b22200000002", comment)
	fetchSocialCommit(t, fork, "b22200000002", comment)
	counts, err := RefreshInteractionCounts(repo, "b22200000001", branch)
	if err != nil {
		t.Fatalf("RefreshInteractionCounts() error = %v", err)
	}
	if counts.Comments != 1 {
		t.Errorf("root comments with a fork mirror of the comment = %d, want 1", counts.Comments)
	}
}

func TestFailureWithDetails(t *testing.T) {
	r := failureWithDetails[string]("CODE", "message", "details")
	if r.Success {
		t.Error("should not succeed")
	}
	if r.Error.Code != "CODE" {
		t.Errorf("Code = %q", r.Error.Code)
	}
	if r.Error.Message != "message" {
		t.Errorf("Message = %q", r.Error.Message)
	}
}

func TestGetSocialItems_byBranch(t *testing.T) {
	setupTestDB(t)
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: "br_112345678", RepoURL: itemsTestRepoURL, Branch: "dev",
		AuthorName: "Test", AuthorEmail: "t@t.com", Message: "test",
		Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "br_112345678", Branch: "dev", Type: "post"})

	items, err := getSocialItems(socialQuery{RepoURL: itemsTestRepoURL, Branch: "dev"})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1, got %d", len(items))
	}
}

func TestInsertSocialItem_repostInteraction(t *testing.T) {
	setupTestDB(t)
	insertItemsTestCommit(t, itemsTestRepoURL, "rpi_root1234")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "rpi_root1234", Branch: itemsTestBranch, Type: "post"})
	insertItemsTestCommit(t, itemsTestRepoURL, "rpi_rpst1234")
	InsertSocialItem(SocialItem{
		RepoURL: itemsTestRepoURL, Hash: "rpi_rpst1234", Branch: itemsTestBranch, Type: "repost",
		OriginalRepoURL: cache.ToNullString(itemsTestRepoURL),
		OriginalHash:    cache.ToNullString("rpi_root1234"),
		OriginalBranch:  cache.ToNullString(itemsTestBranch),
	})
	counts, err := RefreshInteractionCounts(itemsTestRepoURL, "rpi_root1234", itemsTestBranch)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Reposts != 1 {
		t.Errorf("Reposts = %d, want 1", counts.Reposts)
	}
}

func TestInsertSocialItem_quoteInteraction(t *testing.T) {
	setupTestDB(t)
	insertItemsTestCommit(t, itemsTestRepoURL, "qti_root1234")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "qti_root1234", Branch: itemsTestBranch, Type: "post"})
	insertItemsTestCommit(t, itemsTestRepoURL, "qti_quot1234")
	InsertSocialItem(SocialItem{
		RepoURL: itemsTestRepoURL, Hash: "qti_quot1234", Branch: itemsTestBranch, Type: "quote",
		OriginalRepoURL: cache.ToNullString(itemsTestRepoURL),
		OriginalHash:    cache.ToNullString("qti_root1234"),
		OriginalBranch:  cache.ToNullString(itemsTestBranch),
	})
	counts, err := RefreshInteractionCounts(itemsTestRepoURL, "qti_root1234", itemsTestBranch)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Quotes != 1 {
		t.Errorf("Quotes = %d, want 1", counts.Quotes)
	}
}

func TestInsertSocialItem_replyToDifferentFromOriginal(t *testing.T) {
	setupTestDB(t)
	// root -> comment1 -> comment2 (reply_to=comment1, original=root)
	insertItemsTestCommit(t, itemsTestRepoURL, "rtd_root1234")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "rtd_root1234", Branch: itemsTestBranch, Type: "post"})
	insertItemsTestCommit(t, itemsTestRepoURL, "rtd_cmnt1234")
	InsertSocialItem(SocialItem{
		RepoURL: itemsTestRepoURL, Hash: "rtd_cmnt1234", Branch: itemsTestBranch, Type: "comment",
		OriginalRepoURL: cache.ToNullString(itemsTestRepoURL), OriginalHash: cache.ToNullString("rtd_root1234"), OriginalBranch: cache.ToNullString(itemsTestBranch),
		ReplyToRepoURL: cache.ToNullString(itemsTestRepoURL), ReplyToHash: cache.ToNullString("rtd_root1234"), ReplyToBranch: cache.ToNullString(itemsTestBranch),
	})
	insertItemsTestCommit(t, itemsTestRepoURL, "rtd_nest1234")
	InsertSocialItem(SocialItem{
		RepoURL: itemsTestRepoURL, Hash: "rtd_nest1234", Branch: itemsTestBranch, Type: "comment",
		OriginalRepoURL: cache.ToNullString(itemsTestRepoURL), OriginalHash: cache.ToNullString("rtd_root1234"), OriginalBranch: cache.ToNullString(itemsTestBranch),
		ReplyToRepoURL: cache.ToNullString(itemsTestRepoURL), ReplyToHash: cache.ToNullString("rtd_cmnt1234"), ReplyToBranch: cache.ToNullString(itemsTestBranch),
	})
	// Check comment1 got incremented (reply_to != original)
	counts, _ := RefreshInteractionCounts(itemsTestRepoURL, "rtd_cmnt1234", itemsTestBranch)
	if counts.Comments != 1 {
		t.Errorf("comment1 Comments = %d, want 1", counts.Comments)
	}
}

func TestUpdateAncestorInteractions_deepChain(t *testing.T) {
	setupTestDB(t)
	// root -> c1 -> c2 -> c3 (should increment c1's count via ancestor walk)
	insertItemsTestCommit(t, itemsTestRepoURL, "anc_root1234")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "anc_root1234", Branch: itemsTestBranch, Type: "post"})
	insertItemsTestCommit(t, itemsTestRepoURL, "anc_c1__1234")
	InsertSocialItem(SocialItem{
		RepoURL: itemsTestRepoURL, Hash: "anc_c1__1234", Branch: itemsTestBranch, Type: "comment",
		OriginalRepoURL: cache.ToNullString(itemsTestRepoURL), OriginalHash: cache.ToNullString("anc_root1234"), OriginalBranch: cache.ToNullString(itemsTestBranch),
		ReplyToRepoURL: cache.ToNullString(itemsTestRepoURL), ReplyToHash: cache.ToNullString("anc_root1234"), ReplyToBranch: cache.ToNullString(itemsTestBranch),
	})
	insertItemsTestCommit(t, itemsTestRepoURL, "anc_c2__1234")
	InsertSocialItem(SocialItem{
		RepoURL: itemsTestRepoURL, Hash: "anc_c2__1234", Branch: itemsTestBranch, Type: "comment",
		OriginalRepoURL: cache.ToNullString(itemsTestRepoURL), OriginalHash: cache.ToNullString("anc_root1234"), OriginalBranch: cache.ToNullString(itemsTestBranch),
		ReplyToRepoURL: cache.ToNullString(itemsTestRepoURL), ReplyToHash: cache.ToNullString("anc_c1__1234"), ReplyToBranch: cache.ToNullString(itemsTestBranch),
	})
	insertItemsTestCommit(t, itemsTestRepoURL, "anc_c3__1234")
	InsertSocialItem(SocialItem{
		RepoURL: itemsTestRepoURL, Hash: "anc_c3__1234", Branch: itemsTestBranch, Type: "comment",
		OriginalRepoURL: cache.ToNullString(itemsTestRepoURL), OriginalHash: cache.ToNullString("anc_root1234"), OriginalBranch: cache.ToNullString(itemsTestBranch),
		ReplyToRepoURL: cache.ToNullString(itemsTestRepoURL), ReplyToHash: cache.ToNullString("anc_c2__1234"), ReplyToBranch: cache.ToNullString(itemsTestBranch),
	})
	// c1 should have been incremented by ancestor walk from c3
	counts, _ := RefreshInteractionCounts(itemsTestRepoURL, "anc_c1__1234", itemsTestBranch)
	if counts.Comments < 1 {
		t.Errorf("c1 Comments = %d, expected >= 1 from ancestor walk", counts.Comments)
	}
}

func TestGetSocialItemByRef_defaultBranch(t *testing.T) {
	setupTestDB(t)
	insertItemsTestCommit(t, itemsTestRepoURL, "db0a12345678")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "db0a12345678", Branch: itemsTestBranch, Type: "post"})
	// Ref without branch should default to "main"
	refStr := itemsTestRepoURL + "#commit:db0a12345678"
	item, err := GetSocialItemByRef(refStr, "")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if item == nil {
		t.Fatal("should find item with default branch")
	}
}

func TestGetSocialItemByRef_withWorkspaceURL(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/ws/byref"
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: "a0ef12345678", RepoURL: wsURL, Branch: "main",
		AuthorName: "T", AuthorEmail: "t@t.com", Message: "t",
		Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	InsertSocialItem(SocialItem{RepoURL: wsURL, Hash: "a0ef12345678", Branch: "main", Type: "post"})
	// Ref without repo URL should use workspaceURL
	item, err := GetSocialItemByRef("#commit:a0ef12345678@main", wsURL)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if item == nil {
		t.Fatal("should find item with workspace URL fallback")
	}
}

func TestGetTimeline_withListIDs(t *testing.T) {
	setupTestDB(t)
	cache.ExecLocked(func(db *sql.DB) error {
		db.Exec(`INSERT INTO core_lists (id, name, source, version, workdir) VALUES (?, ?, ?, ?, ?)`,
			"tl-list-ids", "Test", "local", "0.1.0", "/tmp")
		db.Exec(`INSERT INTO core_list_repositories (list_id, repo_url, branch) VALUES (?, ?, ?)`,
			"tl-list-ids", itemsTestRepoURL, itemsTestBranch)
		return nil
	})
	insertItemsTestCommit(t, itemsTestRepoURL, "tlli12345678")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "tlli12345678", Branch: itemsTestBranch, Type: "post"})
	items, err := getTimeline([]string{"tl-list-ids"}, "", "", nil, 10, "")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1, got %d", len(items))
	}
}

func TestGetTimeline_withListIDsAndWorkspace(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/ws/timeline"
	cache.ExecLocked(func(db *sql.DB) error {
		db.Exec(`INSERT INTO core_lists (id, name, source, version, workdir) VALUES (?, ?, ?, ?, ?)`,
			"tl-both", "Test", "local", "0.1.0", "/tmp")
		db.Exec(`INSERT INTO core_list_repositories (list_id, repo_url, branch) VALUES (?, ?, ?)`,
			"tl-both", itemsTestRepoURL, itemsTestBranch)
		return nil
	})
	insertItemsTestCommit(t, itemsTestRepoURL, "tlbth_list_1")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "tlbth_list_1", Branch: itemsTestBranch, Type: "post"})
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: "tlbth_ws___1", RepoURL: wsURL, Branch: "main",
		AuthorName: "T", AuthorEmail: "t@t.com", Message: "t",
		Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	InsertSocialItem(SocialItem{RepoURL: wsURL, Hash: "tlbth_ws___1", Branch: "main", Type: "post"})
	items, err := getTimeline([]string{"tl-both"}, wsURL, wsURL, nil, 10, "")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) < 2 {
		t.Errorf("expected at least 2 (list + workspace), got %d", len(items))
	}
}

func TestGetTimeline_withForks(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/ws/forktl"
	forkURL := "https://codeberg.org/fork/forktl"

	// Workspace post.
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: "tlfork_ws_01", RepoURL: wsURL, Branch: "gitmsg/social",
		AuthorName: "T", AuthorEmail: "t@t.com", Message: "ws post",
		Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	InsertSocialItem(SocialItem{RepoURL: wsURL, Hash: "tlfork_ws_01", Branch: "gitmsg/social", Type: "post"})

	// Fork has the same workspace post duplicated under its own repo_url
	// (forks fetch refs/heads/gitmsg/* verbatim).
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: "tlfork_ws_01", RepoURL: forkURL, Branch: "gitmsg/social",
		AuthorName: "T", AuthorEmail: "t@t.com", Message: "ws post",
		Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}

	// Fork-only commit.
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: "tlfork_uniq1", RepoURL: forkURL, Branch: "gitmsg/pm",
		AuthorName: "U", AuthorEmail: "u@u.com", Message: "fork issue",
		Timestamp: time.Date(2025, 10, 22, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}

	items, err := getTimeline(nil, wsURL, wsURL, []string{forkURL}, 10, "")
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	var sawWSDup, sawForkUniq bool
	for _, it := range items {
		if it.Hash == "tlfork_ws_01" && it.RepoURL == forkURL {
			sawWSDup = true
		}
		if it.Hash == "tlfork_uniq1" && it.RepoURL == forkURL {
			sawForkUniq = true
		}
	}
	if sawWSDup {
		t.Error("fork copy of workspace commit should be deduplicated")
	}
	if !sawForkUniq {
		t.Error("fork-only commit should appear on timeline")
	}
}

func TestCreateVirtualSocialItem_nonCommitRef(t *testing.T) {
	ts := time.Now().Format(time.RFC3339)
	ref := protocol.Ref{
		Ext:      "social",
		Time:     ts,
		Ref:      "#branch:main",
		Metadata: "> content",
	}
	got := createVirtualSocialItem(ref, "https://github.com/a/b", "main")
	if got != nil {
		t.Error("non-commit ref should return nil")
	}
}

// --- Additional DB tests for coverage ---

func TestInsertSocialItem_editCommitSkipsInteractions(t *testing.T) {
	setupTestDB(t)
	// Insert canonical post
	insertItemsTestCommit(t, itemsTestRepoURL, "edit_orig1234")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "edit_orig1234", Branch: itemsTestBranch, Type: "post"})

	// Insert edit commit referencing the original via core_commits_version
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: "edit_new_1234", RepoURL: itemsTestRepoURL, Branch: itemsTestBranch,
		AuthorName: "Test", AuthorEmail: "test@test.com",
		Message:   "edited content\n\nGitMsg: ext=\"social\"; type=\"post\"; edits=\"#commit:edit_orig1234@main\"; v=\"0.1.0\"",
		Timestamp: time.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	// Reconcile versions so core_commits_version has the edit mapping
	if _, err := cache.ReconcileVersions(); err != nil {
		t.Fatal(err)
	}

	// Insert the edit social item - should NOT update interaction counts
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "edit_new_1234", Branch: itemsTestBranch, Type: "post"})

	// Root should still have 0 comments (edit doesn't count)
	counts, _ := RefreshInteractionCounts(itemsTestRepoURL, "edit_orig1234", itemsTestBranch)
	if counts.Comments != 0 {
		t.Errorf("edit commit should not increment comments, got %d", counts.Comments)
	}
}

func TestGetThread_withWorkspaceURL(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/ws/thread"
	insertItemsTestCommit(t, itemsTestRepoURL, "tw_root12345")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "tw_root12345", Branch: itemsTestBranch, Type: "post"})

	insertItemsTestCommit(t, itemsTestRepoURL, "tw_cmnt12345")
	InsertSocialItem(SocialItem{
		RepoURL: itemsTestRepoURL, Hash: "tw_cmnt12345", Branch: itemsTestBranch, Type: "comment",
		OriginalRepoURL: cache.ToNullString(itemsTestRepoURL), OriginalHash: cache.ToNullString("tw_root12345"), OriginalBranch: cache.ToNullString(itemsTestBranch),
		ReplyToRepoURL: cache.ToNullString(itemsTestRepoURL), ReplyToHash: cache.ToNullString("tw_root12345"), ReplyToBranch: cache.ToNullString(itemsTestBranch),
	})

	items, err := getThread(itemsTestRepoURL, "tw_root12345", itemsTestBranch, wsURL, nil)
	if err != nil {
		t.Fatalf("getThread() error = %v", err)
	}
	if len(items) < 2 {
		t.Errorf("expected at least 2 thread items, got %d", len(items))
	}
}

func TestGetEditHistory_noVersions(t *testing.T) {
	setupTestDB(t)
	// getEditHistory delegates to gitmsg.GetHistory which requires git refs
	// With no version data in cache, it should return empty without error
	items, err := getEditHistory(itemsTestRepoURL, "eh_orig12345", itemsTestBranch, "")
	if err != nil {
		t.Fatalf("getEditHistory() error = %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 versions for non-versioned item, got %d", len(items))
	}
}

func TestGetEditHistoryPosts_noVersions(t *testing.T) {
	setupTestDB(t)
	posts, err := GetEditHistoryPosts(itemsTestRepoURL, "ehp_orig1234", itemsTestBranch, "")
	if err != nil {
		t.Fatalf("GetEditHistoryPosts() error = %v", err)
	}
	if len(posts) != 0 {
		t.Errorf("expected 0 posts for non-versioned item, got %d", len(posts))
	}
}

func TestGetSocialItems_forFollowerCheck(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/ws/follchk"
	insertItemsTestCommit(t, itemsTestRepoURL, "fc_112345678")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "fc_112345678", Branch: itemsTestBranch, Type: "post"})
	// Insert follower
	_ = insertFollower(itemsTestRepoURL, wsURL, "list1", "", time.Now())
	items, err := getSocialItems(socialQuery{RepoURL: itemsTestRepoURL, ForFollowerCheck: wsURL})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1, got %d", len(items))
	}
	if !items[0].FollowsWorkspace {
		t.Error("FollowsWorkspace should be true")
	}
}

// TestGetListPosts_followerMark pins invariant 4: a list scope marks a follower.
func TestGetListPosts_followerMark(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/ws/listfollow"
	repoURL := "https://github.com/list/follower"
	cache.ExecLocked(func(db *sql.DB) error {
		db.Exec(`INSERT INTO core_lists (id, name, source, version, workdir) VALUES (?, ?, ?, ?, ?)`,
			"follow-list", "Follow", "local", "0.1.0", "/tmp")
		db.Exec(`INSERT INTO core_list_repositories (list_id, repo_url, branch) VALUES (?, ?, ?)`,
			"follow-list", repoURL, itemsTestBranch)
		return nil
	})
	insertItemsTestCommit(t, repoURL, "c11100000001")
	InsertSocialItem(SocialItem{RepoURL: repoURL, Hash: "c11100000001", Branch: itemsTestBranch, Type: "post"})
	if err := insertFollower(repoURL, wsURL, "follow-list", "", time.Now()); err != nil {
		t.Fatalf("insertFollower() error = %v", err)
	}

	result := getListPosts("follow-list", wsURL, &GetPostsOptions{})
	if !result.Success {
		t.Fatalf("getListPosts() failed: %s", result.Error.Message)
	}
	if len(result.Data) != 1 {
		t.Fatalf("posts = %d, want 1", len(result.Data))
	}
	if !result.Data[0].Display.FollowsYou {
		t.Error("FollowsYou = false for a list post from a follower, want true")
	}
}

func TestInsertSocialItem_repostAncestorInteraction(t *testing.T) {
	setupTestDB(t)
	// root -> c1 -> repost (should count as repost on c1 ancestor)
	insertItemsTestCommit(t, itemsTestRepoURL, "rpa_root1234")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "rpa_root1234", Branch: itemsTestBranch, Type: "post"})
	insertItemsTestCommit(t, itemsTestRepoURL, "rpa_c1__1234")
	InsertSocialItem(SocialItem{
		RepoURL: itemsTestRepoURL, Hash: "rpa_c1__1234", Branch: itemsTestBranch, Type: "comment",
		OriginalRepoURL: cache.ToNullString(itemsTestRepoURL), OriginalHash: cache.ToNullString("rpa_root1234"), OriginalBranch: cache.ToNullString(itemsTestBranch),
		ReplyToRepoURL: cache.ToNullString(itemsTestRepoURL), ReplyToHash: cache.ToNullString("rpa_root1234"), ReplyToBranch: cache.ToNullString(itemsTestBranch),
	})
	insertItemsTestCommit(t, itemsTestRepoURL, "rpa_rp__1234")
	InsertSocialItem(SocialItem{
		RepoURL: itemsTestRepoURL, Hash: "rpa_rp__1234", Branch: itemsTestBranch, Type: "repost",
		OriginalRepoURL: cache.ToNullString(itemsTestRepoURL), OriginalHash: cache.ToNullString("rpa_root1234"), OriginalBranch: cache.ToNullString(itemsTestBranch),
		ReplyToRepoURL: cache.ToNullString(itemsTestRepoURL), ReplyToHash: cache.ToNullString("rpa_c1__1234"), ReplyToBranch: cache.ToNullString(itemsTestBranch),
	})
	counts, _ := RefreshInteractionCounts(itemsTestRepoURL, "rpa_root1234", itemsTestBranch)
	if counts.Reposts < 1 {
		t.Errorf("root Reposts = %d, want >= 1", counts.Reposts)
	}
}

func TestInsertSocialItem_quoteAncestorInteraction(t *testing.T) {
	setupTestDB(t)
	// root -> c1 -> quote (should count as quote on root via ancestor walk)
	insertItemsTestCommit(t, itemsTestRepoURL, "qta_root1234")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "qta_root1234", Branch: itemsTestBranch, Type: "post"})
	insertItemsTestCommit(t, itemsTestRepoURL, "qta_c1__1234")
	InsertSocialItem(SocialItem{
		RepoURL: itemsTestRepoURL, Hash: "qta_c1__1234", Branch: itemsTestBranch, Type: "comment",
		OriginalRepoURL: cache.ToNullString(itemsTestRepoURL), OriginalHash: cache.ToNullString("qta_root1234"), OriginalBranch: cache.ToNullString(itemsTestBranch),
		ReplyToRepoURL: cache.ToNullString(itemsTestRepoURL), ReplyToHash: cache.ToNullString("qta_root1234"), ReplyToBranch: cache.ToNullString(itemsTestBranch),
	})
	insertItemsTestCommit(t, itemsTestRepoURL, "qta_qt__1234")
	InsertSocialItem(SocialItem{
		RepoURL: itemsTestRepoURL, Hash: "qta_qt__1234", Branch: itemsTestBranch, Type: "quote",
		OriginalRepoURL: cache.ToNullString(itemsTestRepoURL), OriginalHash: cache.ToNullString("qta_root1234"), OriginalBranch: cache.ToNullString(itemsTestBranch),
		ReplyToRepoURL: cache.ToNullString(itemsTestRepoURL), ReplyToHash: cache.ToNullString("qta_c1__1234"), ReplyToBranch: cache.ToNullString(itemsTestBranch),
	})
	counts, _ := RefreshInteractionCounts(itemsTestRepoURL, "qta_root1234", itemsTestBranch)
	if counts.Quotes < 1 {
		t.Errorf("root Quotes = %d, want >= 1", counts.Quotes)
	}
}

// --- Query filter coverage tests ---

func TestGetTimeline_noUnionsReturnsNil(t *testing.T) {
	setupTestDB(t)
	// No list IDs and no workspace URL → empty unions → returns nil
	items, err := getTimeline(nil, "", "", nil, 0, "")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if items != nil {
		t.Errorf("expected nil for no unions, got %d items", len(items))
	}
}

func TestGetTimeline_withLimit(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/tl/limit"
	for i := 0; i < 5; i++ {
		h := fmt.Sprintf("tll_%08d", i)
		insertItemsTestCommit(t, wsURL, h)
		InsertSocialItem(SocialItem{RepoURL: wsURL, Hash: h, Branch: itemsTestBranch, Type: "post"})
	}
	items, err := getTimeline(nil, wsURL, wsURL, nil, 2, "")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) > 2 {
		t.Errorf("expected at most 2 with limit, got %d", len(items))
	}
}

func TestInsertSocialItem_upgradeFromVirtual(t *testing.T) {
	setupTestDB(t)
	// Insert virtual item first
	err := InsertSocialItem(SocialItem{
		RepoURL:     itemsTestRepoURL,
		Hash:        "vup_12345678",
		Branch:      itemsTestBranch,
		Type:        "post",
		AuthorName:  "Virtual",
		AuthorEmail: "v@t.com",
		Content:     "virtual content",
		Timestamp:   time.Now(),
		IsVirtual:   true,
	})
	if err != nil {
		t.Fatalf("insert virtual: %v", err)
	}
	// Insert non-virtual item with same key → should upgrade
	err = InsertSocialItem(SocialItem{
		RepoURL: itemsTestRepoURL,
		Hash:    "vup_12345678",
		Branch:  itemsTestBranch,
		Type:    "post",
	})
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	// Verify is_virtual cleared
	isVirtual, _ := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var v int
		err := db.QueryRow(`SELECT is_virtual FROM core_commits WHERE repo_url = ? AND hash = ? AND branch = ?`,
			itemsTestRepoURL, "vup_12345678", itemsTestBranch).Scan(&v)
		return v, err
	})
	if isVirtual != 0 {
		t.Error("expected virtual flag cleared after upgrade")
	}
}
