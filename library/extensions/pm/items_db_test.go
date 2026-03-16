// items_db_test.go - Tests for PM item database operations
package pm

import (
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
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
	insertPMTestCommit(t, repoURL, "filt_1234567")
	InsertPMItem(PMItem{RepoURL: repoURL, Hash: "filt_1234567", Branch: branch, Type: "issue", State: "open",
		Labels: cache.ToNullString("priority/high")})
	insertPMTestCommit(t, repoURL, "filt_2345678")
	InsertPMItem(PMItem{RepoURL: repoURL, Hash: "filt_2345678", Branch: branch, Type: "issue", State: "open",
		Labels: cache.ToNullString("priority/low")})

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
	insertPMTestCommit(t, repoURL, "lbl1_1234567")
	InsertPMItem(PMItem{RepoURL: repoURL, Hash: "lbl1_1234567", Branch: branch, Type: "issue", State: "open",
		Labels: cache.ToNullString("priority/high,kind/bug")})
	insertPMTestCommit(t, repoURL, "lbl2_1234567")
	InsertPMItem(PMItem{RepoURL: repoURL, Hash: "lbl2_1234567", Branch: branch, Type: "issue", State: "open",
		Labels: cache.ToNullString("priority/low")})

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
