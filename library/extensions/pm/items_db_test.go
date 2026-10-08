// items_db_test.go - Tests for PM item database operations
package pm

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
)

const pmTestBranch = "gitmsg/pm"

func insertPMTestCommit(t *testing.T, repoURL, hash string) {
	t.Helper()
	if err := cache.InsertCommits([]cache.Commit{{
		Hash:        hash,
		RepoURL:     repoURL,
		Branch:      pmTestBranch,
		AuthorName:  "Test User",
		AuthorEmail: "test@test.com",
		Message:     "test commit",
		Timestamp:   time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
}

// insertPMTestCommitWithLabels caches an issue commit of the test repository whose header carries the labels, the column the view reads.
func insertPMTestCommitWithLabels(t *testing.T, hash, labels string) {
	t.Helper()
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: hash, RepoURL: "https://github.com/test/repo", Branch: pmTestBranch, AuthorName: "Test User", AuthorEmail: "test@test.com",
		Message:   "Issue\n\nGitMsg: ext=\"pm\"; type=\"issue\"; labels=\"" + labels + "\"; v=\"0.1.0\"",
		Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
}

func TestInsertPMItem(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/repo"
	hash := "ins123456789"
	branch := pmTestBranch
	insertPMTestCommit(t, repoURL, hash)

	err := InsertPMItem(PMItem{
		RepoURL: repoURL,
		Hash:    hash,
		Branch:  branch,
		Type:    "issue",
		State:   "open",
	})
	if err != nil {
		t.Fatalf("InsertPMItem() error = %v", err)
	}
}

func TestInsertPMItem_upsert(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/repo"
	hash := "ups123456789"
	branch := pmTestBranch
	insertPMTestCommit(t, repoURL, hash)

	item := PMItem{
		RepoURL: repoURL,
		Hash:    hash,
		Branch:  branch,
		Type:    "issue",
		State:   "open",
	}
	if err := InsertPMItem(item); err != nil {
		t.Fatalf("first InsertPMItem() error = %v", err)
	}
	item.State = "closed"
	if err := InsertPMItem(item); err != nil {
		t.Fatalf("second InsertPMItem() error = %v", err)
	}
	got := queryPMItem(t, hash)
	if got.State != "closed" {
		t.Errorf("State = %q after upsert, want closed", got.State)
	}
}

func TestGetPMItem_notFound(t *testing.T) {
	setupTestDB(t)
	_, err := GetPMItem("https://github.com/test/repo", "nonexistent12", "gitmsg/pm")
	if err == nil {
		t.Error("GetPMItem() should return error for non-existent item")
	}
}

func TestGetPMItems_filterByType(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/repo"
	branch := pmTestBranch
	for i, typ := range []string{"issue", "issue", "milestone"} {
		hash := []string{"iss1_1234567", "iss2_1234567", "mil1_1234567"}[i]
		insertPMTestCommit(t, repoURL, hash)
		if err := InsertPMItem(PMItem{RepoURL: repoURL, Hash: hash, Branch: branch, Type: typ, State: "open"}); err != nil {
			t.Fatalf("InsertPMItem() error = %v", err)
		}
	}

	items, err := GetPMItems(PMQuery{Types: []string{"issue"}, RepoURL: repoURL, Branch: branch})
	if err != nil {
		t.Fatalf("GetPMItems() error = %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 issues, got %d", len(items))
	}
	for _, item := range items {
		if item.Type != "issue" {
			t.Errorf("Type = %q, want issue", item.Type)
		}
	}
}

func TestGetPMItems_filterByState(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/repo"
	branch := pmTestBranch
	hashes := []string{"open_12345678", "closed_1234567"}
	states := []string{"open", "closed"}
	for i := range hashes {
		insertPMTestCommit(t, repoURL, hashes[i])
		if err := InsertPMItem(PMItem{RepoURL: repoURL, Hash: hashes[i], Branch: branch, Type: "issue", State: states[i]}); err != nil {
			t.Fatalf("InsertPMItem() error = %v", err)
		}
	}

	items, err := GetPMItems(PMQuery{Types: []string{"issue"}, States: []string{"open"}, RepoURL: repoURL, Branch: branch})
	if err != nil {
		t.Fatalf("GetPMItems() error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 open issue, got %d", len(items))
	}
}

func TestGetPMItems_filterByRepo(t *testing.T) {
	setupTestDB(t)
	branch := "gitmsg/pm"
	repo1 := "https://github.com/user/repo1"
	repo2 := "https://github.com/user/repo2"
	insertPMTestCommit(t, repo1, "r1hash123456")
	insertPMTestCommit(t, repo2, "r2hash123456")
	InsertPMItem(PMItem{RepoURL: repo1, Hash: "r1hash123456", Branch: branch, Type: "issue", State: "open"})
	InsertPMItem(PMItem{RepoURL: repo2, Hash: "r2hash123456", Branch: branch, Type: "issue", State: "open"})

	items, err := GetPMItems(PMQuery{RepoURL: repo1, Branch: branch})
	if err != nil {
		t.Fatalf("GetPMItems() error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item for repo1, got %d", len(items))
	}
}

func TestGetPMItems_limit(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/repo"
	branch := pmTestBranch
	for i := 0; i < 5; i++ {
		hash := []string{"lim1_1234567", "lim2_1234567", "lim3_1234567", "lim4_1234567", "lim5_1234567"}[i]
		insertPMTestCommit(t, repoURL, hash)
		InsertPMItem(PMItem{RepoURL: repoURL, Hash: hash, Branch: branch, Type: "issue", State: "open"})
	}

	items, err := GetPMItems(PMQuery{RepoURL: repoURL, Branch: branch, Limit: 2})
	if err != nil {
		t.Fatalf("GetPMItems() error = %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items with limit=2, got %d", len(items))
	}
}

func TestGetIssues_result(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/repo"
	branch := "gitmsg/pm"
	hash := "issres123456"
	insertPMTestCommit(t, repoURL, hash)
	InsertPMItem(PMItem{RepoURL: repoURL, Hash: hash, Branch: branch, Type: "issue", State: "open"})

	result := GetIssues(repoURL, branch, nil, "", 10)
	if !result.Success {
		t.Fatalf("GetIssues() failed: %s", result.Error.Message)
	}
	if len(result.Data) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(result.Data))
	}
}

func TestGetMilestones_result(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/repo"
	branch := "gitmsg/pm"
	hash := "milres123456"
	insertPMTestCommit(t, repoURL, hash)
	InsertPMItem(PMItem{RepoURL: repoURL, Hash: hash, Branch: branch, Type: "milestone", State: "open"})

	result := GetMilestones(repoURL, branch, nil, "", 10)
	if !result.Success {
		t.Fatalf("GetMilestones() failed: %s", result.Error.Message)
	}
	if len(result.Data) != 1 {
		t.Fatalf("expected 1 milestone, got %d", len(result.Data))
	}
}

func TestGetSprints_result(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/repo"
	branch := "gitmsg/pm"
	hash := "sprres123456"
	insertPMTestCommit(t, repoURL, hash)
	InsertPMItem(PMItem{RepoURL: repoURL, Hash: hash, Branch: branch, Type: "sprint", State: "planned",
		StartDate: cache.ToNullString("2025-11-01"), EndDate: cache.ToNullString("2025-11-14")})

	result := GetSprints(repoURL, branch, nil, "", 10)
	if !result.Success {
		t.Fatalf("GetSprints() failed: %s", result.Error.Message)
	}
	if len(result.Data) != 1 {
		t.Fatalf("expected 1 sprint, got %d", len(result.Data))
	}
}

func TestGetPMItems_filterStr(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/repo"
	branch := pmTestBranch
	insertPMTestCommitWithLabels(t, "filt_1234567", "priority/high")
	InsertPMItem(PMItem{RepoURL: repoURL, Hash: "filt_1234567", Branch: branch, Type: "issue", State: "open"})
	insertPMTestCommitWithLabels(t, "filt_2345678", "priority/low")
	InsertPMItem(PMItem{RepoURL: repoURL, Hash: "filt_2345678", Branch: branch, Type: "issue", State: "open"})

	items, err := GetPMItems(PMQuery{RepoURL: repoURL, Branch: branch, FilterStr: "priority:high"})
	if err != nil {
		t.Fatalf("GetPMItems() error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item with priority:high filter, got %d", len(items))
	}
}

func TestGetPMItems_sinceUntil(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/repo"
	branch := pmTestBranch
	hash := "sinc_1234567"
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: hash, RepoURL: repoURL, Branch: branch,
		AuthorName: "Test", AuthorEmail: "test@test.com",
		Message:   "test",
		Timestamp: time.Date(2025, 10, 15, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	InsertPMItem(PMItem{RepoURL: repoURL, Hash: hash, Branch: branch, Type: "issue", State: "open"})

	since := time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2025, 10, 20, 0, 0, 0, 0, time.UTC)
	items, err := GetPMItems(PMQuery{RepoURL: repoURL, Branch: branch, Since: &since, Until: &until})
	if err != nil {
		t.Fatalf("GetPMItems() error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item in date range, got %d", len(items))
	}

	before := time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC)
	items2, err := GetPMItems(PMQuery{RepoURL: repoURL, Branch: branch, Until: &before})
	if err != nil {
		t.Fatalf("GetPMItems() error = %v", err)
	}
	if len(items2) != 0 {
		t.Errorf("expected 0 items before date, got %d", len(items2))
	}
}

func TestGetPMItems_offset(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/repo"
	branch := pmTestBranch
	for i := 0; i < 3; i++ {
		hash := []string{"off1_1234567", "off2_1234567", "off3_1234567"}[i]
		insertPMTestCommit(t, repoURL, hash)
		InsertPMItem(PMItem{RepoURL: repoURL, Hash: hash, Branch: branch, Type: "issue", State: "open"})
	}
	items, err := GetPMItems(PMQuery{RepoURL: repoURL, Branch: branch, Limit: 2, Offset: 1})
	if err != nil {
		t.Fatalf("GetPMItems() error = %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items with offset=1 limit=2, got %d", len(items))
	}
}

func TestGetPMItems_labelsFilter(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/repo"
	branch := pmTestBranch
	insertPMTestCommitWithLabels(t, "lbl1_1234567", "priority/high,kind/bug")
	InsertPMItem(PMItem{RepoURL: repoURL, Hash: "lbl1_1234567", Branch: branch, Type: "issue", State: "open"})
	insertPMTestCommitWithLabels(t, "lbl2_1234567", "priority/low")
	InsertPMItem(PMItem{RepoURL: repoURL, Hash: "lbl2_1234567", Branch: branch, Type: "issue", State: "open"})

	items, err := GetPMItems(PMQuery{RepoURL: repoURL, Branch: branch, Labels: []string{"kind/bug"}})
	if err != nil {
		t.Fatalf("GetPMItems() error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item with kind/bug label, got %d", len(items))
	}
}

func TestGetPMItems_assigneeFilter(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/repo"
	branch := pmTestBranch
	insertPMTestCommit(t, repoURL, "asn1_1234567")
	InsertPMItem(PMItem{RepoURL: repoURL, Hash: "asn1_1234567", Branch: branch, Type: "issue", State: "open",
		Assignees: cache.ToNullString("alice@test.com")})
	insertPMTestCommit(t, repoURL, "asn2_1234567")
	InsertPMItem(PMItem{RepoURL: repoURL, Hash: "asn2_1234567", Branch: branch, Type: "issue", State: "open",
		Assignees: cache.ToNullString("bob@test.com")})

	items, err := GetPMItems(PMQuery{RepoURL: repoURL, Branch: branch, Assignee: "alice@test.com"})
	if err != nil {
		t.Fatalf("GetPMItems() error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item for alice, got %d", len(items))
	}
}

func TestGetPMItemByRef_emptyRef(t *testing.T) {
	setupTestDB(t)
	_, err := GetPMItemByRef("", "https://github.com/test/repo")
	if err == nil {
		t.Error("GetPMItemByRef with empty ref should return error")
	}
}

func TestGetPMItemByRef_noRepoInRef(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/repo"
	hash := "aabb11cc2233"
	branch := pmTestBranch
	insertPMTestCommit(t, repoURL, hash)
	InsertPMItem(PMItem{RepoURL: repoURL, Hash: hash, Branch: branch, Type: "issue", State: "open"})

	// Ref without repo URL should use defaultRepoURL
	item, err := GetPMItemByRef("#commit:"+hash+"@gitmsg/pm", repoURL)
	if err != nil {
		t.Fatalf("GetPMItemByRef() error = %v", err)
	}
	if item.Hash != hash {
		t.Errorf("Hash = %q, want %q", item.Hash, hash)
	}
}

func TestGetPMItemByRef(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/repo"
	hash := "aabb11223344"
	branch := pmTestBranch
	insertPMTestCommit(t, repoURL, hash)
	InsertPMItem(PMItem{RepoURL: repoURL, Hash: hash, Branch: branch, Type: "issue", State: "open"})

	refStr := "https://github.com/test/repo#commit:" + hash + "@gitmsg/pm"
	item, err := GetPMItemByRef(refStr, repoURL)
	if err != nil {
		t.Fatalf("GetPMItemByRef() error = %v", err)
	}
	if item == nil {
		t.Fatal("GetPMItemByRef() returned nil")
	}
	if item.Hash != hash {
		t.Errorf("Hash = %q, want %q", item.Hash, hash)
	}
}

// TestGetIssue_ambiguousPrefix refuses a prefix that two issues share and ignores a milestone that shares it.
func TestGetIssue_ambiguousPrefix(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://example.com/test/ambiguous"
	items := map[string]string{"a1b2c3d4e5f6": "issue", "a1f6e5d4c3b2": "issue", "a1c3c3c3c3c3": "milestone"}
	for hash, typ := range items {
		insertPMTestCommit(t, repoURL, hash)
		if err := InsertPMItem(PMItem{RepoURL: repoURL, Hash: hash, Branch: pmTestBranch, Type: typ, State: "open"}); err != nil {
			t.Fatalf("InsertPMItem() error = %v", err)
		}
	}
	res := GetIssue("a1")
	if res.Success || res.Error.Code != "NOT_FOUND" {
		t.Fatalf("GetIssue() of an ambiguous prefix = %+v, want NOT_FOUND", res)
	}
	for _, hash := range []string{"a1b2c3d4e5f6", "a1f6e5d4c3b2"} {
		if !strings.Contains(res.Error.Message, hash) {
			t.Errorf("the message should name %s, got %q", hash, res.Error.Message)
		}
	}
	if got := GetIssue("a1b"); !got.Success {
		t.Errorf("GetIssue() of a prefix matching one issue failed: %s", got.Error.Message)
	}
	if got := GetMilestone("a1"); !got.Success {
		t.Errorf("GetMilestone() of a prefix matching one milestone failed: %s", got.Error.Message)
	}
}

// markStale sets stale_since on one cached row.
func markStale(t *testing.T, repoURL, hash, branch string) {
	t.Helper()
	if err := cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`UPDATE core_commits SET stale_since = '2025-10-22T00:00:00Z' WHERE repo_url = ? AND hash = ? AND branch = ?`, repoURL, hash, branch)
		return err
	}); err != nil {
		t.Fatalf("mark stale: %v", err)
	}
}

// insertPMTestCommitOn caches one commit under the given branch.
func insertPMTestCommitOn(t *testing.T, repoURL, hash, branch string) {
	t.Helper()
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: hash, RepoURL: repoURL, Branch: branch, AuthorName: "Test User", AuthorEmail: "test@test.com",
		Message: "test commit", Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
}

// TestGetPMItems_excludesStaleCommit pins invariant 1: a list and its count leave out an issue whose row is stale.
func TestGetPMItems_excludesStaleCommit(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/stale"
	for _, hash := range []string{"5a1e00000001", "5a1e00000002"} {
		insertPMTestCommit(t, repoURL, hash)
		if err := InsertPMItem(PMItem{RepoURL: repoURL, Hash: hash, Branch: pmTestBranch, Type: "issue", State: "open"}); err != nil {
			t.Fatalf("InsertPMItem() error = %v", err)
		}
	}
	markStale(t, repoURL, "5a1e00000002", pmTestBranch)

	q := PMQuery{Types: []string{"issue"}, RepoURL: repoURL}
	items, err := GetPMItems(q)
	if err != nil {
		t.Fatalf("GetPMItems() error = %v", err)
	}
	if len(items) != 1 || items[0].Hash != "5a1e00000001" {
		t.Errorf("GetPMItems() = %d items, want the one live issue", len(items))
	}
	if count, err := GetPMItemsCount(q); err != nil || count != 1 {
		t.Errorf("GetPMItemsCount() = %d, %v, want 1", count, err)
	}
}

// TestGetPMItems_excludesActionRows: an edit that has a timeline action is still in no issue list.
func TestGetPMItems_excludesActionRows(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/actions"
	commit := func(hash, header string, minute int) cache.Commit {
		return cache.Commit{
			Hash: hash, RepoURL: repoURL, Branch: pmTestBranch, AuthorName: "Test User", AuthorEmail: "test@test.com",
			Message:   "Issue\n\nGitMsg: ext=\"pm\"; type=\"issue\"; " + header + "; v=\"0.1.0\"",
			Timestamp: time.Date(2025, 10, 21, 12, minute, 0, 0, time.UTC),
		}
	}
	if err := cache.InsertCommits([]cache.Commit{
		commit("ac7100000001", `state="open"`, 0),
		commit("ac7100000002", `edits="#commit:ac7100000001@`+pmTestBranch+`"; state="closed"`, 1),
	}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
	for hash, state := range map[string]string{"ac7100000001": "open", "ac7100000002": "closed"} {
		if err := InsertPMItem(PMItem{RepoURL: repoURL, Hash: hash, Branch: pmTestBranch, Type: "issue", State: state}); err != nil {
			t.Fatalf("InsertPMItem() error = %v", err)
		}
	}
	cache.SyncEditExtensionFields([]cache.EditKey{{RepoURL: repoURL, Hash: "ac7100000002", Branch: pmTestBranch}})

	q := PMQuery{Types: []string{"issue"}, RepoURL: repoURL}
	items, err := GetPMItems(q)
	if err != nil {
		t.Fatalf("GetPMItems() error = %v", err)
	}
	if len(items) != 1 || items[0].Hash != "ac7100000001" || items[0].State != "closed" {
		t.Errorf("GetPMItems() = %+v, want the one issue, closed, and no row for the edit", items)
	}
	if count, err := GetPMItemsCount(q); err != nil || count != 1 {
		t.Errorf("GetPMItemsCount() = %d, %v, want 1", count, err)
	}
}

// TestGetPMItemByHashPrefix_liveFirst pins invariant 2: a prefix lookup opens the live row of a moved issue, and still finds a stale-only one.
func TestGetPMItemByHashPrefix_liveFirst(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/test/prefix"
	const moved, gone = "11fe00000001", "11fe00000002"
	for _, row := range []struct{ hash, branch string }{{moved, "feature/x"}, {moved, pmTestBranch}, {gone, "feature/x"}} {
		insertPMTestCommitOn(t, repoURL, row.hash, row.branch)
		if err := InsertPMItem(PMItem{RepoURL: repoURL, Hash: row.hash, Branch: row.branch, Type: "issue", State: "open"}); err != nil {
			t.Fatalf("InsertPMItem() error = %v", err)
		}
	}
	markStale(t, repoURL, moved, "feature/x")
	markStale(t, repoURL, gone, "feature/x")

	if _, err := GetPMItemByHashPrefix("11fe000000", ""); err == nil {
		t.Fatal("GetPMItemByHashPrefix() accepted a prefix two issues share")
	}
	item, err := GetPMItemByHashPrefix("11fe00000001", "")
	if err != nil || item.Branch != pmTestBranch {
		t.Errorf("GetPMItemByHashPrefix(moved) = %+v, %v, want the live row under %s", item, err, pmTestBranch)
	}
	item, err = GetPMItemByHashPrefix("11fe00000002", "")
	if err != nil || item.Branch != "feature/x" {
		t.Errorf("GetPMItemByHashPrefix(gone) = %+v, %v, want the stale row", item, err)
	}
}
