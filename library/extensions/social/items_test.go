// items_test.go - Tests for social item queries, conversions, and cache operations
package social

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

// --- Pure function tests ---

func TestCreateVirtualSocialItem_wrongExt(t *testing.T) {
	ref := protocol.Ref{Ext: "pm", Metadata: "> Hello", Ref: "#commit:abc123@main"}
	if got := CreateVirtualSocialItem(ref, "https://github.com/a/b", "main"); got != nil {
		t.Error("wrong ext should return nil")
	}
}

func TestCreateVirtualSocialItem_noMetadata(t *testing.T) {
	ref := protocol.Ref{Ext: "social", Metadata: "", Ref: "#commit:abc123@main"}
	if got := CreateVirtualSocialItem(ref, "https://github.com/a/b", "main"); got != nil {
		t.Error("empty metadata should return nil")
	}
}

func TestCreateVirtualSocialItem_noContent(t *testing.T) {
	ref := protocol.Ref{Ext: "social", Metadata: "not a quote line", Ref: "#commit:abc123@main"}
	if got := CreateVirtualSocialItem(ref, "https://github.com/a/b", "main"); got != nil {
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
	if got := CreateVirtualSocialItem(ref, "https://github.com/a/b", "main"); got != nil {
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
	got := CreateVirtualSocialItem(ref, "https://github.com/fallback/repo", "develop")
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
	got := CreateVirtualSocialItem(ref, "https://github.com/a/b", "develop")
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
	got := CreateVirtualSocialItem(ref, "https://github.com/a/b", "main")
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
	if post.Repository != "myrepository" {
		t.Errorf("empty repoURL should default to myrepository, got %q", post.Repository)
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

	item, err := GetCachedCommit(itemsTestRepoURL, "ccmt12345678", itemsTestBranch)
	if err != nil {
		t.Fatalf("GetCachedCommit() error = %v", err)
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
	_, err := GetCachedCommit("https://github.com/no/repo", "nonexistent", "main")
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

	items, err := GetSocialItems(SocialQuery{Types: []string{"post"}, RepoURL: itemsTestRepoURL})
	if err != nil {
		t.Fatalf("GetSocialItems() error = %v", err)
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

	items, err := GetSocialItems(SocialQuery{RepoURL: itemsTestRepoURL, Limit: 2, Offset: 1})
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
	items, err := GetSocialItems(SocialQuery{RepoURL: itemsTestRepoURL, Since: &since, Until: &until})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item in range, got %d", len(items))
	}

	before := time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)
	items2, err := GetSocialItems(SocialQuery{RepoURL: itemsTestRepoURL, Until: &before})
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

	items, err := GetTimeline(nil, wsURL, 10, "")
	if err != nil {
		t.Fatalf("GetTimeline() error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 timeline item, got %d", len(items))
	}
}

func TestGetTimeline_empty(t *testing.T) {
	setupTestDB(t)
	items, err := GetTimeline(nil, "", 10, "")
	if err != nil {
		t.Fatalf("GetTimeline(empty) error = %v", err)
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

	items, err := GetThread(itemsTestRepoURL, "thrd_root123", itemsTestBranch, "")
	if err != nil {
		t.Fatalf("GetThread() error = %v", err)
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

	parents, err := GetParentChain(itemsTestRepoURL, "pc_grch12345", itemsTestBranch, "")
	if err != nil {
		t.Fatalf("GetParentChain() error = %v", err)
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

func TestFailureWithDetails(t *testing.T) {
	r := FailureWithDetails[string]("CODE", "message", "details")
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

	items, err := GetSocialItems(SocialQuery{RepoURL: itemsTestRepoURL, Branch: "dev"})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1, got %d", len(items))
	}
}

func TestGetSocialItems_byRepoURLs(t *testing.T) {
	setupTestDB(t)
	insertItemsTestCommit(t, "https://github.com/multi/a", "mlt_a1234567")
	InsertSocialItem(SocialItem{RepoURL: "https://github.com/multi/a", Hash: "mlt_a1234567", Branch: itemsTestBranch, Type: "post"})
	insertItemsTestCommit(t, "https://github.com/multi/b", "mlt_b1234567")
	InsertSocialItem(SocialItem{RepoURL: "https://github.com/multi/b", Hash: "mlt_b1234567", Branch: itemsTestBranch, Type: "post"})

	items, err := GetSocialItems(SocialQuery{RepoURLs: []string{"https://github.com/multi/a", "https://github.com/multi/b"}})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2, got %d", len(items))
	}
}

func TestGetSocialItems_byOriginal(t *testing.T) {
	setupTestDB(t)
	insertItemsTestCommit(t, itemsTestRepoURL, "orig1234567a")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "orig1234567a", Branch: itemsTestBranch, Type: "post"})
	insertItemsTestCommit(t, itemsTestRepoURL, "cmto1234567a")
	InsertSocialItem(SocialItem{
		RepoURL: itemsTestRepoURL, Hash: "cmto1234567a", Branch: itemsTestBranch, Type: "comment",
		OriginalRepoURL: cache.ToNullString(itemsTestRepoURL),
		OriginalHash:    cache.ToNullString("orig1234567a"),
		OriginalBranch:  cache.ToNullString(itemsTestBranch),
	})

	items, err := GetSocialItems(SocialQuery{
		OriginalRepoURL: itemsTestRepoURL,
		OriginalHash:    "orig1234567a",
		OriginalBranch:  itemsTestBranch,
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 comment, got %d", len(items))
	}
}

func TestGetSocialItems_byListID(t *testing.T) {
	setupTestDB(t)
	// Insert a list repo mapping
	cache.ExecLocked(func(db *sql.DB) error {
		db.Exec(`INSERT INTO core_lists (id, name, source, version, workdir) VALUES (?, ?, ?, ?, ?)`,
			"test-list-q", "Test", "local", "0.1.0", "/tmp")
		db.Exec(`INSERT INTO core_list_repositories (list_id, repo_url, branch) VALUES (?, ?, ?)`,
			"test-list-q", itemsTestRepoURL, itemsTestBranch)
		return nil
	})
	insertItemsTestCommit(t, itemsTestRepoURL, "lstq1234567a")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "lstq1234567a", Branch: itemsTestBranch, Type: "post"})

	items, err := GetSocialItems(SocialQuery{ListID: "test-list-q"})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item in list, got %d", len(items))
	}
}

func TestGetSocialItems_byRepos(t *testing.T) {
	setupTestDB(t)
	insertItemsTestCommit(t, itemsTestRepoURL, "rps_12345678")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "rps_12345678", Branch: itemsTestBranch, Type: "post"})

	items, err := GetSocialItems(SocialQuery{Repos: []RepoRef{{URL: itemsTestRepoURL, Branch: itemsTestBranch}}})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1, got %d", len(items))
	}
}

func TestGetAllItems_basic(t *testing.T) {
	setupTestDB(t)
	insertItemsTestCommit(t, itemsTestRepoURL, "gai_12345678")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "gai_12345678", Branch: itemsTestBranch, Type: "post"})

	items, err := GetAllItems(SocialQuery{RepoURL: itemsTestRepoURL})
	if err != nil {
		t.Fatalf("GetAllItems() error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item, got %d", len(items))
	}
}

func TestGetAllItems_withListIDs(t *testing.T) {
	setupTestDB(t)
	cache.ExecLocked(func(db *sql.DB) error {
		db.Exec(`INSERT INTO core_lists (id, name, source, version, workdir) VALUES (?, ?, ?, ?, ?)`,
			"all-items-list", "Test", "local", "0.1.0", "/tmp")
		db.Exec(`INSERT INTO core_list_repositories (list_id, repo_url, branch) VALUES (?, ?, ?)`,
			"all-items-list", itemsTestRepoURL, itemsTestBranch)
		return nil
	})
	insertItemsTestCommit(t, itemsTestRepoURL, "ail_12345678")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "ail_12345678", Branch: itemsTestBranch, Type: "post"})

	items, err := GetAllItems(SocialQuery{ListIDs: []string{"all-items-list"}})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) < 1 {
		t.Errorf("expected at least 1 item, got %d", len(items))
	}
}

func TestGetAllItems_branchFilter(t *testing.T) {
	setupTestDB(t)
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: "gab_12345678", RepoURL: itemsTestRepoURL, Branch: "dev",
		AuthorName: "Test", AuthorEmail: "t@t.com", Message: "test",
		Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "gab_12345678", Branch: "dev", Type: "post"})
	items, err := GetAllItems(SocialQuery{RepoURL: itemsTestRepoURL, Branch: "dev"})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1, got %d", len(items))
	}
}

func TestGetAllItems_repoURLsFilter(t *testing.T) {
	setupTestDB(t)
	for _, url := range []string{"https://github.com/gaurl/a", "https://github.com/gaurl/b"} {
		hash := "gaurl_" + url[len(url)-1:]
		if err := cache.InsertCommits([]cache.Commit{{
			Hash: hash, RepoURL: url, Branch: itemsTestBranch,
			AuthorName: "T", AuthorEmail: "t@t.com", Message: "t",
			Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
		}}); err != nil {
			t.Fatal(err)
		}
		InsertSocialItem(SocialItem{RepoURL: url, Hash: hash, Branch: itemsTestBranch, Type: "post"})
	}
	items, err := GetAllItems(SocialQuery{RepoURLs: []string{"https://github.com/gaurl/a", "https://github.com/gaurl/b"}})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2, got %d", len(items))
	}
}

func TestGetAllItems_listIDFilter(t *testing.T) {
	setupTestDB(t)
	cache.ExecLocked(func(db *sql.DB) error {
		db.Exec(`INSERT INTO core_lists (id, name, source, version, workdir) VALUES (?, ?, ?, ?, ?)`,
			"ga-list-id", "Test", "local", "0.1.0", "/tmp")
		db.Exec(`INSERT INTO core_list_repositories (list_id, repo_url, branch) VALUES (?, ?, ?)`,
			"ga-list-id", itemsTestRepoURL, itemsTestBranch)
		return nil
	})
	insertItemsTestCommit(t, itemsTestRepoURL, "gali1234567a")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "gali1234567a", Branch: itemsTestBranch, Type: "post"})
	items, err := GetAllItems(SocialQuery{ListID: "ga-list-id"})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1, got %d", len(items))
	}
}

func TestGetAllItems_sinceUntilFilter(t *testing.T) {
	setupTestDB(t)
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: "gasu12345678", RepoURL: itemsTestRepoURL, Branch: itemsTestBranch,
		AuthorName: "T", AuthorEmail: "t@t.com", Message: "t",
		Timestamp: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "gasu12345678", Branch: itemsTestBranch, Type: "post"})
	since := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2025, 6, 30, 0, 0, 0, 0, time.UTC)
	items, err := GetAllItems(SocialQuery{RepoURL: itemsTestRepoURL, Since: &since, Until: &until})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1, got %d", len(items))
	}
}

func TestGetAllItems_offsetFilter(t *testing.T) {
	setupTestDB(t)
	for i := 0; i < 5; i++ {
		hash := "gaof" + string(rune('a'+i)) + "_123456"
		if err := cache.InsertCommits([]cache.Commit{{
			Hash: hash, RepoURL: itemsTestRepoURL, Branch: itemsTestBranch,
			AuthorName: "T", AuthorEmail: "t@t.com", Message: "t",
			Timestamp: time.Date(2025, 10, 21, 12, i, 0, 0, time.UTC),
		}}); err != nil {
			t.Fatal(err)
		}
		InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: hash, Branch: itemsTestBranch, Type: "post"})
	}
	items, err := GetAllItems(SocialQuery{RepoURL: itemsTestRepoURL, Limit: 2, Offset: 1})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 with limit=2 offset=1, got %d", len(items))
	}
}

func TestGetAllItems_listIDsAndWorkspace(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/ws/allitems"
	cache.ExecLocked(func(db *sql.DB) error {
		db.Exec(`INSERT INTO core_lists (id, name, source, version, workdir) VALUES (?, ?, ?, ?, ?)`,
			"gailw-list", "Test", "local", "0.1.0", "/tmp")
		db.Exec(`INSERT INTO core_list_repositories (list_id, repo_url, branch) VALUES (?, ?, ?)`,
			"gailw-list", itemsTestRepoURL, itemsTestBranch)
		return nil
	})
	insertItemsTestCommit(t, itemsTestRepoURL, "gailw_list_1")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "gailw_list_1", Branch: itemsTestBranch, Type: "post"})
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: "gailw_ws___1", RepoURL: wsURL, Branch: "gitmsg/social",
		AuthorName: "T", AuthorEmail: "t@t.com", Message: "t",
		Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	InsertSocialItem(SocialItem{RepoURL: wsURL, Hash: "gailw_ws___1", Branch: "gitmsg/social", Type: "post"})
	items, err := GetAllItems(SocialQuery{ListIDs: []string{"gailw-list"}, WorkspaceURL: wsURL})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) < 2 {
		t.Errorf("expected at least 2 (list + workspace), got %d", len(items))
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

func TestGetSocialItems_listIDsAndWorkspaceURL(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/ws/items"
	cache.ExecLocked(func(db *sql.DB) error {
		db.Exec(`INSERT INTO core_lists (id, name, source, version, workdir) VALUES (?, ?, ?, ?, ?)`,
			"gsi-lw-list", "Test", "local", "0.1.0", "/tmp")
		db.Exec(`INSERT INTO core_list_repositories (list_id, repo_url, branch) VALUES (?, ?, ?)`,
			"gsi-lw-list", itemsTestRepoURL, itemsTestBranch)
		return nil
	})
	insertItemsTestCommit(t, itemsTestRepoURL, "gsilw_list_1")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "gsilw_list_1", Branch: itemsTestBranch, Type: "post"})
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: "gsilw_ws___1", RepoURL: wsURL, Branch: itemsTestBranch,
		AuthorName: "T", AuthorEmail: "t@t.com", Message: "t",
		Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	InsertSocialItem(SocialItem{RepoURL: wsURL, Hash: "gsilw_ws___1", Branch: itemsTestBranch, Type: "post"})
	items, err := GetSocialItems(SocialQuery{ListIDs: []string{"gsi-lw-list"}, WorkspaceURL: wsURL})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) < 2 {
		t.Errorf("expected at least 2, got %d", len(items))
	}
}

func TestGetSocialItems_reposWithoutBranch(t *testing.T) {
	setupTestDB(t)
	insertItemsTestCommit(t, itemsTestRepoURL, "rpnb12345678")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "rpnb12345678", Branch: itemsTestBranch, Type: "post"})
	items, err := GetSocialItems(SocialQuery{Repos: []RepoRef{{URL: itemsTestRepoURL}}})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1, got %d", len(items))
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
	items, err := GetTimeline([]string{"tl-list-ids"}, "", 10, "")
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
	items, err := GetTimeline([]string{"tl-both"}, wsURL, 10, "")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) < 2 {
		t.Errorf("expected at least 2 (list + workspace), got %d", len(items))
	}
}

func TestGetSocialItems_originalWithoutBranch(t *testing.T) {
	setupTestDB(t)
	insertItemsTestCommit(t, itemsTestRepoURL, "onb_root1234")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "onb_root1234", Branch: itemsTestBranch, Type: "post"})
	insertItemsTestCommit(t, itemsTestRepoURL, "onb_cmnt1234")
	InsertSocialItem(SocialItem{
		RepoURL: itemsTestRepoURL, Hash: "onb_cmnt1234", Branch: itemsTestBranch, Type: "comment",
		OriginalRepoURL: cache.ToNullString(itemsTestRepoURL),
		OriginalHash:    cache.ToNullString("onb_root1234"),
	})
	// Query by original without branch
	items, err := GetSocialItems(SocialQuery{OriginalRepoURL: itemsTestRepoURL, OriginalHash: "onb_root1234"})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1, got %d", len(items))
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
	got := CreateVirtualSocialItem(ref, "https://github.com/a/b", "main")
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

func TestResolveCurrentVersion_notFound(t *testing.T) {
	setupTestDB(t)
	_, err := ResolveCurrentVersion(itemsTestRepoURL, "nonexistent12", itemsTestBranch, "")
	if err == nil {
		t.Error("expected error for nonexistent item")
	}
}

func TestResolveCurrentVersion_basicPost(t *testing.T) {
	setupTestDB(t)
	insertItemsTestCommit(t, itemsTestRepoURL, "rcv_12345678")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "rcv_12345678", Branch: itemsTestBranch, Type: "post"})
	resolved, err := ResolveCurrentVersion(itemsTestRepoURL, "rcv_12345678", itemsTestBranch, "")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if resolved.Item == nil {
		t.Fatal("Item should not be nil")
	}
	if resolved.Item.Hash != "rcv_12345678" {
		t.Errorf("Hash = %q", resolved.Item.Hash)
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

	items, err := GetThread(itemsTestRepoURL, "tw_root12345", itemsTestBranch, wsURL)
	if err != nil {
		t.Fatalf("GetThread() error = %v", err)
	}
	if len(items) < 2 {
		t.Errorf("expected at least 2 thread items, got %d", len(items))
	}
}

func TestGetEditHistory_noVersions(t *testing.T) {
	setupTestDB(t)
	// GetEditHistory delegates to gitmsg.GetHistory which requires git refs
	// With no version data in cache, it should return empty without error
	items, err := GetEditHistory(itemsTestRepoURL, "eh_orig12345", itemsTestBranch, "")
	if err != nil {
		t.Fatalf("GetEditHistory() error = %v", err)
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
	_ = InsertFollower(itemsTestRepoURL, wsURL, "list1", "", time.Now())
	items, err := GetSocialItems(SocialQuery{RepoURL: itemsTestRepoURL, ForFollowerCheck: wsURL})
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

func TestGetAllItems_forFollowerCheck(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/ws/allfc"
	insertItemsTestCommit(t, itemsTestRepoURL, "afc_12345678")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "afc_12345678", Branch: itemsTestBranch, Type: "post"})
	_ = InsertFollower(itemsTestRepoURL, wsURL, "list1", "", time.Now())
	items, err := GetAllItems(SocialQuery{RepoURL: itemsTestRepoURL, ForFollowerCheck: wsURL})
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
	items, err := GetTimeline(nil, "", 0, "")
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
	items, err := GetTimeline(nil, wsURL, 2, "")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) > 2 {
		t.Errorf("expected at most 2 with limit, got %d", len(items))
	}
}

func TestGetSocialItems_withReposFilter(t *testing.T) {
	setupTestDB(t)
	repoA := "https://github.com/repos/filter-a"
	repoB := "https://github.com/repos/filter-b"
	insertItemsTestCommit(t, repoA, "rfa_12345678")
	InsertSocialItem(SocialItem{RepoURL: repoA, Hash: "rfa_12345678", Branch: itemsTestBranch, Type: "post"})
	insertItemsTestCommit(t, repoB, "rfb_12345678")
	InsertSocialItem(SocialItem{RepoURL: repoB, Hash: "rfb_12345678", Branch: "dev", Type: "post"})

	// Filter with branch
	items, err := GetSocialItems(SocialQuery{
		Repos: []RepoRef{{URL: repoA, Branch: itemsTestBranch}},
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1, got %d", len(items))
	}

	// Filter without branch
	items2, err := GetSocialItems(SocialQuery{
		Repos: []RepoRef{{URL: repoB}},
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items2) != 1 {
		t.Errorf("expected 1 without branch filter, got %d", len(items2))
	}
}

func TestGetSocialItems_withListIDsAndWorkspace(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/lidws/workspace"
	listRepo := "https://github.com/lidws/listed"
	insertItemsTestCommit(t, wsURL, "liw_12345678")
	InsertSocialItem(SocialItem{RepoURL: wsURL, Hash: "liw_12345678", Branch: itemsTestBranch, Type: "post"})
	insertItemsTestCommit(t, listRepo, "lir_12345678")
	InsertSocialItem(SocialItem{RepoURL: listRepo, Hash: "lir_12345678", Branch: itemsTestBranch, Type: "post"})
	// Insert list with repo
	_ = cache.ExecLocked(func(db *sql.DB) error {
		db.Exec(`INSERT INTO core_lists (id, name, source, version, workdir) VALUES (?, ?, ?, ?, ?)`,
			"lid-list", "LID", "local", "0.1.0", "/tmp")
		db.Exec(`INSERT INTO core_list_repositories (list_id, repo_url, branch) VALUES (?, ?, ?)`,
			"lid-list", listRepo, itemsTestBranch)
		return nil
	})
	items, err := GetSocialItems(SocialQuery{
		ListIDs:      []string{"lid-list"},
		WorkspaceURL: wsURL,
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) < 2 {
		t.Errorf("expected at least 2 (workspace + list), got %d", len(items))
	}
}

func TestGetSocialItems_withOriginalFilter(t *testing.T) {
	setupTestDB(t)
	origRepo := "https://github.com/orig/filter"
	origHash := "of_012345678"
	insertItemsTestCommit(t, origRepo, origHash)
	InsertSocialItem(SocialItem{RepoURL: origRepo, Hash: origHash, Branch: itemsTestBranch, Type: "post"})
	// Insert a comment on this post
	insertItemsTestCommit(t, itemsTestRepoURL, "ofc_12345678")
	InsertSocialItem(SocialItem{
		RepoURL: itemsTestRepoURL, Hash: "ofc_12345678", Branch: itemsTestBranch, Type: "comment",
		OriginalRepoURL: cache.ToNullString(origRepo), OriginalHash: cache.ToNullString(origHash), OriginalBranch: cache.ToNullString(itemsTestBranch),
	})
	// Query by original with branch
	items, err := GetSocialItems(SocialQuery{
		OriginalRepoURL: origRepo, OriginalHash: origHash, OriginalBranch: itemsTestBranch,
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 comment, got %d", len(items))
	}
	// Query by original without branch
	items2, err := GetSocialItems(SocialQuery{
		OriginalRepoURL: origRepo, OriginalHash: origHash,
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items2) != 1 {
		t.Errorf("expected 1 without branch, got %d", len(items2))
	}
}

func TestGetSocialItems_withOffset(t *testing.T) {
	setupTestDB(t)
	for i := 0; i < 3; i++ {
		h := fmt.Sprintf("ofs_%08d", i)
		insertItemsTestCommit(t, itemsTestRepoURL, h)
		InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: h, Branch: itemsTestBranch, Type: "post"})
	}
	items, err := GetSocialItems(SocialQuery{RepoURL: itemsTestRepoURL, Offset: 1, Limit: 2})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) > 2 {
		t.Errorf("expected at most 2 with limit+offset, got %d", len(items))
	}
}

func TestGetAllItems_withBranchFilter(t *testing.T) {
	setupTestDB(t)
	insertItemsTestCommit(t, itemsTestRepoURL, "brf_12345678")
	InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: "brf_12345678", Branch: "feature", Type: "post"})
	items, err := GetAllItems(SocialQuery{RepoURL: itemsTestRepoURL, Branch: "feature"})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	for _, item := range items {
		if item.Branch != "feature" {
			t.Errorf("expected branch 'feature', got %q", item.Branch)
		}
	}
}

func TestGetAllItems_withRepoURLs(t *testing.T) {
	setupTestDB(t)
	repo1 := "https://github.com/urls/repo1"
	repo2 := "https://github.com/urls/repo2"
	insertItemsTestCommit(t, repo1, "ur1_12345678")
	InsertSocialItem(SocialItem{RepoURL: repo1, Hash: "ur1_12345678", Branch: itemsTestBranch, Type: "post"})
	insertItemsTestCommit(t, repo2, "ur2_12345678")
	InsertSocialItem(SocialItem{RepoURL: repo2, Hash: "ur2_12345678", Branch: itemsTestBranch, Type: "post"})
	items, err := GetAllItems(SocialQuery{RepoURLs: []string{repo1, repo2}})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2, got %d", len(items))
	}
}

func TestGetAllItems_withListIDsAndWorkspace(t *testing.T) {
	setupTestDB(t)
	wsURL := "https://github.com/allws/workspace"
	listRepo := "https://github.com/allws/listed"
	insertItemsTestCommit(t, wsURL, "alw_12345678")
	InsertSocialItem(SocialItem{RepoURL: wsURL, Hash: "alw_12345678", Branch: itemsTestBranch, Type: "post"})
	insertItemsTestCommit(t, listRepo, "alr_12345678")
	InsertSocialItem(SocialItem{RepoURL: listRepo, Hash: "alr_12345678", Branch: itemsTestBranch, Type: "post"})
	_ = cache.ExecLocked(func(db *sql.DB) error {
		db.Exec(`INSERT INTO core_lists (id, name, source, version, workdir) VALUES (?, ?, ?, ?, ?)`,
			"all-list", "All", "local", "0.1.0", "/tmp")
		db.Exec(`INSERT INTO core_list_repositories (list_id, repo_url, branch) VALUES (?, ?, ?)`,
			"all-list", listRepo, itemsTestBranch)
		return nil
	})
	items, err := GetAllItems(SocialQuery{
		ListIDs:      []string{"all-list"},
		WorkspaceURL: wsURL,
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) < 2 {
		t.Errorf("expected at least 2, got %d", len(items))
	}
}

func TestGetAllItems_withOffset(t *testing.T) {
	setupTestDB(t)
	for i := 0; i < 3; i++ {
		h := fmt.Sprintf("aof_%08d", i)
		insertItemsTestCommit(t, itemsTestRepoURL, h)
		InsertSocialItem(SocialItem{RepoURL: itemsTestRepoURL, Hash: h, Branch: itemsTestBranch, Type: "post"})
	}
	items, err := GetAllItems(SocialQuery{RepoURL: itemsTestRepoURL, Offset: 1, Limit: 1})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) > 1 {
		t.Errorf("expected at most 1 with offset+limit, got %d", len(items))
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

func TestGetSocialItems_withListIDOnly(t *testing.T) {
	setupTestDB(t)
	repo := "https://github.com/listonly/repo"
	insertItemsTestCommit(t, repo, "lio_12345678")
	InsertSocialItem(SocialItem{RepoURL: repo, Hash: "lio_12345678", Branch: itemsTestBranch, Type: "post"})
	_ = cache.ExecLocked(func(db *sql.DB) error {
		db.Exec(`INSERT INTO core_lists (id, name, source, version, workdir) VALUES (?, ?, ?, ?, ?)`,
			"lio-list", "LIO", "local", "0.1.0", "/tmp")
		db.Exec(`INSERT INTO core_list_repositories (list_id, repo_url, branch) VALUES (?, ?, ?)`,
			"lio-list", repo, itemsTestBranch)
		return nil
	})
	items, err := GetSocialItems(SocialQuery{ListID: "lio-list"})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1, got %d", len(items))
	}
}

func TestGetAllItems_withListIDOnly(t *testing.T) {
	setupTestDB(t)
	repo := "https://github.com/alio/repo"
	insertItemsTestCommit(t, repo, "ali_12345678")
	InsertSocialItem(SocialItem{RepoURL: repo, Hash: "ali_12345678", Branch: itemsTestBranch, Type: "post"})
	_ = cache.ExecLocked(func(db *sql.DB) error {
		db.Exec(`INSERT INTO core_lists (id, name, source, version, workdir) VALUES (?, ?, ?, ?, ?)`,
			"ali-list", "ALI", "local", "0.1.0", "/tmp")
		db.Exec(`INSERT INTO core_list_repositories (list_id, repo_url, branch) VALUES (?, ?, ?)`,
			"ali-list", repo, itemsTestBranch)
		return nil
	})
	items, err := GetAllItems(SocialQuery{ListID: "ali-list"})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1, got %d", len(items))
	}
}
