// sync_test.go - Tests for PM commit processing
package pm

import (
	"database/sql"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/protocol"

	_ "github.com/gitsocial-org/gitsocial/extensions/social"
)

func setupTestDB(t *testing.T) {
	t.Helper()
	cache.Reset()
	dir := t.TempDir()
	if err := cache.Open(dir); err != nil {
		t.Fatalf("cache.Open() error = %v", err)
	}
	t.Cleanup(func() {
		cache.Reset()
		cache.Open(testCacheDir)
	})
}

const pmSyncTestRepoURL = "https://github.com/test/repo"
const pmSyncTestBranch = "gitmsg/pm"

func insertTestCommit(t *testing.T, hash, message string) {
	t.Helper()
	if err := cache.InsertCommits([]cache.Commit{{
		Hash:        hash,
		RepoURL:     pmSyncTestRepoURL,
		Branch:      pmSyncTestBranch,
		AuthorName:  "Test User",
		AuthorEmail: "test@test.com",
		Message:     message,
		Timestamp:   time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
}

func TestProcessPMCommit_nilMessage(t *testing.T) {
	setupTestDB(t)
	gc := git.Commit{Hash: "abc123456789"}
	processPMCommit(gc, nil, "https://github.com/test/repo", "gitmsg/pm")
	count := countPMItems(t)
	if count != 0 {
		t.Errorf("expected 0 pm_items, got %d", count)
	}
}

func TestProcessPMCommit_wrongExtension(t *testing.T) {
	setupTestDB(t)
	msg := &protocol.Message{
		Content: "A social post",
		Header:  protocol.Header{Ext: "social", V: "0.1.0", Fields: map[string]string{"type": "post"}},
	}
	gc := git.Commit{Hash: "abc123456789"}
	processPMCommit(gc, msg, "https://github.com/test/repo", "gitmsg/pm")
	count := countPMItems(t)
	if count != 0 {
		t.Errorf("expected 0 pm_items for wrong extension, got %d", count)
	}
}

func TestProcessPMCommit_missingType(t *testing.T) {
	setupTestDB(t)
	msg := &protocol.Message{
		Content: "PM item without type",
		Header:  protocol.Header{Ext: "pm", V: "0.1.0", Fields: map[string]string{"state": "open"}},
	}
	gc := git.Commit{Hash: "abc123456789"}
	processPMCommit(gc, msg, "https://github.com/test/repo", "gitmsg/pm")
	count := countPMItems(t)
	if count != 0 {
		t.Errorf("expected 0 pm_items for missing type, got %d", count)
	}
}

func TestProcessPMCommit_basicIssue(t *testing.T) {
	setupTestDB(t)
	repoURL := pmSyncTestRepoURL
	hash := "abc123456789"
	branch := pmSyncTestBranch
	content := "Fix the login bug\n\n" + `--- GitMsg: ext="pm"; type="issue"; state="open"; v="0.1.0" ---`
	insertTestCommit(t, hash, content)

	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: hash, Timestamp: time.Now()}
	processPMCommit(gc, msg, repoURL, branch)

	item := queryPMItem(t, hash)
	if item.Type != "issue" {
		t.Errorf("Type = %q, want issue", item.Type)
	}
	if item.State != "open" {
		t.Errorf("State = %q, want open", item.State)
	}
}

func TestProcessPMCommit_defaultState(t *testing.T) {
	setupTestDB(t)
	repoURL := pmSyncTestRepoURL
	hash := "def456789abc"
	branch := pmSyncTestBranch
	content := "No state specified\n\n" + `--- GitMsg: ext="pm"; type="issue"; v="0.1.0" ---`
	insertTestCommit(t, hash, content)

	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: hash, Timestamp: time.Now()}
	processPMCommit(gc, msg, repoURL, branch)

	item := queryPMItem(t, hash)
	if item.State != "open" {
		t.Errorf("State = %q, want open (default)", item.State)
	}
}

func TestProcessPMCommit_withLabelsAssignees(t *testing.T) {
	setupTestDB(t)
	repoURL := pmSyncTestRepoURL
	hash := "lab123456789"
	branch := pmSyncTestBranch
	content := "Labeled issue\n\n" + `--- GitMsg: ext="pm"; type="issue"; state="open"; labels="priority/high,kind/bug"; assignees="alice@test.com,bob@test.com"; v="0.1.0" ---`
	insertTestCommit(t, hash, content)

	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: hash, Timestamp: time.Now()}
	processPMCommit(gc, msg, repoURL, branch)

	item := queryPMItem(t, hash)
	if !item.Labels.Valid || item.Labels.String != "priority/high,kind/bug" {
		t.Errorf("Labels = %v, want priority/high,kind/bug", item.Labels)
	}
	if !item.Assignees.Valid || item.Assignees.String != "alice@test.com,bob@test.com" {
		t.Errorf("Assignees = %v, want alice@test.com,bob@test.com", item.Assignees)
	}
}

func TestProcessPMCommit_withMilestoneRef(t *testing.T) {
	setupTestDB(t)
	repoURL := pmSyncTestRepoURL
	hash := "aaa111222333"
	branch := pmSyncTestBranch
	content := "Issue with milestone\n\n" + `--- GitMsg: ext="pm"; type="issue"; state="open"; milestone="#commit:bbb444555666@gitmsg/pm"; v="0.1.0" ---`
	insertTestCommit(t, hash, content)

	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: hash, Timestamp: time.Now()}
	processPMCommit(gc, msg, repoURL, branch)

	item := queryPMItem(t, hash)
	if !item.MilestoneHash.Valid || item.MilestoneHash.String != "bbb444555666" {
		t.Errorf("MilestoneHash = %v, want bbb444555666", item.MilestoneHash)
	}
}

func TestProcessPMCommit_withSprintRef(t *testing.T) {
	setupTestDB(t)
	repoURL := pmSyncTestRepoURL
	hash := "ccc111222333"
	branch := pmSyncTestBranch
	content := "Issue in sprint\n\n" + `--- GitMsg: ext="pm"; type="issue"; state="open"; sprint="#commit:ddd444555666@gitmsg/pm"; v="0.1.0" ---`
	insertTestCommit(t, hash, content)

	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: hash, Timestamp: time.Now()}
	processPMCommit(gc, msg, repoURL, branch)

	item := queryPMItem(t, hash)
	if !item.SprintHash.Valid || item.SprintHash.String != "ddd444555666" {
		t.Errorf("SprintHash = %v, want ddd444555666", item.SprintHash)
	}
}

func TestProcessPMCommit_withParentRef(t *testing.T) {
	setupTestDB(t)
	repoURL := pmSyncTestRepoURL
	hash := "eee111222333"
	branch := pmSyncTestBranch
	content := "Sub-issue\n\n" + `--- GitMsg: ext="pm"; type="issue"; state="open"; parent="#commit:fff444555666@gitmsg/pm"; v="0.1.0" ---`
	insertTestCommit(t, hash, content)

	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: hash, Timestamp: time.Now()}
	processPMCommit(gc, msg, repoURL, branch)

	item := queryPMItem(t, hash)
	if !item.ParentHash.Valid || item.ParentHash.String != "fff444555666" {
		t.Errorf("ParentHash = %v, want fff444555666", item.ParentHash)
	}
}

func TestProcessPMCommit_withRootRef(t *testing.T) {
	setupTestDB(t)
	repoURL := pmSyncTestRepoURL
	hash := "aab111222333"
	branch := pmSyncTestBranch
	content := "Deep sub-issue\n\n" + `--- GitMsg: ext="pm"; type="issue"; state="open"; root="#commit:aac444555666@gitmsg/pm"; v="0.1.0" ---`
	insertTestCommit(t, hash, content)

	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: hash, Timestamp: time.Now()}
	processPMCommit(gc, msg, repoURL, branch)

	item := queryPMItem(t, hash)
	if !item.RootHash.Valid || item.RootHash.String != "aac444555666" {
		t.Errorf("RootHash = %v, want aac444555666", item.RootHash)
	}
}

func TestProcessPMCommit_withEditsRef(t *testing.T) {
	setupTestDB(t)
	repoURL := pmSyncTestRepoURL
	branch := pmSyncTestBranch
	canonicalHash := "ca0011223344"
	editHash := "ed5566778899"
	// Insert canonical commit first
	canonContent := "Original issue\n\n" + `--- GitMsg: ext="pm"; type="issue"; state="open"; v="0.1.0" ---`
	insertTestCommit(t, canonicalHash, canonContent)
	msg := protocol.ParseMessage(canonContent)
	gc := git.Commit{Hash: canonicalHash, Timestamp: time.Now()}
	processPMCommit(gc, msg, repoURL, branch)

	// Insert edit commit
	editContent := "Updated issue\n\n" + `--- GitMsg: ext="pm"; type="issue"; state="closed"; edits="#commit:ca0011223344@gitmsg/pm"; v="0.1.0" ---`
	insertTestCommit(t, editHash, editContent)
	editMsg := protocol.ParseMessage(editContent)
	editGc := git.Commit{Hash: editHash, Timestamp: time.Now()}
	processPMCommit(editGc, editMsg, repoURL, branch)

	// Verify version row was created
	count, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_commits_version WHERE edit_hash = ? AND canonical_hash = ?`,
			editHash, canonicalHash).Scan(&c)
		return c, err
	})
	if err != nil {
		t.Fatalf("query error = %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 version row, got %d", count)
	}
}

func TestProcessPMCommit_withFullURLMilestoneRef(t *testing.T) {
	setupTestDB(t)
	repoURL := pmSyncTestRepoURL
	hash := "ful111222333"
	branch := pmSyncTestBranch
	content := "Issue with full URL milestone\n\n" + `--- GitMsg: ext="pm"; type="issue"; state="open"; milestone="https://github.com/other/repo#commit:bbb444555666@gitmsg/pm"; v="0.1.0" ---`
	insertTestCommit(t, hash, content)

	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: hash, Timestamp: time.Now()}
	processPMCommit(gc, msg, repoURL, branch)

	item := queryPMItem(t, hash)
	if !item.MilestoneHash.Valid || item.MilestoneHash.String != "bbb444555666" {
		t.Errorf("MilestoneHash = %v, want bbb444555666", item.MilestoneHash)
	}
	if !item.MilestoneRepoURL.Valid || item.MilestoneRepoURL.String != "https://github.com/other/repo" {
		t.Errorf("MilestoneRepoURL = %v, want other/repo", item.MilestoneRepoURL)
	}
}

func TestProcessPMCommit_withFullURLSprintRef(t *testing.T) {
	setupTestDB(t)
	repoURL := pmSyncTestRepoURL
	hash := "f0a222333444"
	branch := pmSyncTestBranch
	content := "Issue with full URL sprint\n\n" + `--- GitMsg: ext="pm"; type="issue"; state="open"; sprint="https://github.com/other/repo#commit:aaa444555666@gitmsg/pm"; v="0.1.0" ---`
	insertTestCommit(t, hash, content)

	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: hash, Timestamp: time.Now()}
	processPMCommit(gc, msg, repoURL, branch)

	item := queryPMItem(t, hash)
	if !item.SprintHash.Valid || item.SprintHash.String != "aaa444555666" {
		t.Errorf("SprintHash = %v, want aaa444555666", item.SprintHash)
	}
	if !item.SprintRepoURL.Valid || item.SprintRepoURL.String != "https://github.com/other/repo" {
		t.Errorf("SprintRepoURL = %v, want other/repo", item.SprintRepoURL)
	}
}

func TestProcessPMCommit_withFullURLParentRef(t *testing.T) {
	setupTestDB(t)
	repoURL := pmSyncTestRepoURL
	hash := "f0b333444555"
	branch := pmSyncTestBranch
	content := "Issue with full URL parent\n\n" + `--- GitMsg: ext="pm"; type="issue"; state="open"; parent="https://github.com/other/repo#commit:ccc444555666@gitmsg/pm"; v="0.1.0" ---`
	insertTestCommit(t, hash, content)

	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: hash, Timestamp: time.Now()}
	processPMCommit(gc, msg, repoURL, branch)

	item := queryPMItem(t, hash)
	if !item.ParentHash.Valid || item.ParentHash.String != "ccc444555666" {
		t.Errorf("ParentHash = %v, want ccc444555666", item.ParentHash)
	}
	if !item.ParentRepoURL.Valid || item.ParentRepoURL.String != "https://github.com/other/repo" {
		t.Errorf("ParentRepoURL = %v, want other/repo", item.ParentRepoURL)
	}
}

func TestProcessPMCommit_withFullURLRootRef(t *testing.T) {
	setupTestDB(t)
	repoURL := pmSyncTestRepoURL
	hash := "f0c444555666"
	branch := pmSyncTestBranch
	content := "Issue with full URL root\n\n" + `--- GitMsg: ext="pm"; type="issue"; state="open"; root="https://github.com/other/repo#commit:ddd444555666@gitmsg/pm"; v="0.1.0" ---`
	insertTestCommit(t, hash, content)

	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: hash, Timestamp: time.Now()}
	processPMCommit(gc, msg, repoURL, branch)

	item := queryPMItem(t, hash)
	if !item.RootHash.Valid || item.RootHash.String != "ddd444555666" {
		t.Errorf("RootHash = %v, want ddd444555666", item.RootHash)
	}
	if !item.RootRepoURL.Valid || item.RootRepoURL.String != "https://github.com/other/repo" {
		t.Errorf("RootRepoURL = %v, want other/repo", item.RootRepoURL)
	}
}

func TestProcessPMCommit_withEditsFullURLRef(t *testing.T) {
	setupTestDB(t)
	repoURL := pmSyncTestRepoURL
	branch := pmSyncTestBranch
	canonHash := "ca1122334455"
	editHash := "ed6677889900"
	otherRepo := "https://github.com/other/repo"
	otherBranch := "gitmsg/custom"

	// Insert canonical commit at the coordinates the full URL ref points to
	canonContent := "Original\n\n" + `--- GitMsg: ext="pm"; type="issue"; state="open"; v="0.1.0" ---`
	if err := cache.InsertCommits([]cache.Commit{{
		Hash: canonHash, RepoURL: otherRepo, Branch: otherBranch,
		AuthorName: "Test", AuthorEmail: "t@t.com", Message: canonContent,
		Timestamp: time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatal(err)
	}
	msg := protocol.ParseMessage(canonContent)
	processPMCommit(git.Commit{Hash: canonHash, Timestamp: time.Now()}, msg, otherRepo, otherBranch)

	// Insert edit commit on the local repo, referencing canonical via full URL
	editContent := "Updated\n\n" + `--- GitMsg: ext="pm"; type="issue"; state="closed"; edits="https://github.com/other/repo#commit:ca1122334455@gitmsg/custom"; v="0.1.0" ---`
	insertTestCommit(t, editHash, editContent)
	editMsg := protocol.ParseMessage(editContent)
	processPMCommit(git.Commit{Hash: editHash, Timestamp: time.Now()}, editMsg, repoURL, branch)

	// Verify version row exists with correct canonical coordinates
	count, _ := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_commits_version WHERE edit_hash = ? AND canonical_repo_url = ? AND canonical_branch = ?`,
			editHash, otherRepo, otherBranch).Scan(&c)
		return c, err
	})
	if count != 1 {
		t.Errorf("expected 1 version row for edit hash, got %d", count)
	}
}

func TestProcessPMCommit_branchlessRefs(t *testing.T) {
	setupTestDB(t)
	hash := "brf111222333"
	// Refs without @branch suffix trigger the branch fallback paths
	content := "Issue with branchless refs\n\n" + `--- GitMsg: ext="pm"; type="issue"; state="open"; milestone="#commit:aab111222333"; sprint="#commit:aac111222333"; parent="#commit:aad111222333"; root="#commit:aae111222333"; v="0.1.0" ---`
	insertTestCommit(t, hash, content)

	msg := protocol.ParseMessage(content)
	processPMCommit(git.Commit{Hash: hash, Timestamp: time.Now()}, msg, pmSyncTestRepoURL, pmSyncTestBranch)

	item := queryPMItem(t, hash)
	// Branch should default to the commit's branch (pmSyncTestBranch)
	if !item.MilestoneBranch.Valid || item.MilestoneBranch.String != pmSyncTestBranch {
		t.Errorf("MilestoneBranch = %v, want %q", item.MilestoneBranch, pmSyncTestBranch)
	}
	if !item.SprintBranch.Valid || item.SprintBranch.String != pmSyncTestBranch {
		t.Errorf("SprintBranch = %v, want %q", item.SprintBranch, pmSyncTestBranch)
	}
	if !item.ParentBranch.Valid || item.ParentBranch.String != pmSyncTestBranch {
		t.Errorf("ParentBranch = %v, want %q", item.ParentBranch, pmSyncTestBranch)
	}
	if !item.RootBranch.Valid || item.RootBranch.String != pmSyncTestBranch {
		t.Errorf("RootBranch = %v, want %q", item.RootBranch, pmSyncTestBranch)
	}
	// RepoURL should default to the commit's repo
	if !item.MilestoneRepoURL.Valid || item.MilestoneRepoURL.String != pmSyncTestRepoURL {
		t.Errorf("MilestoneRepoURL = %v, want %q", item.MilestoneRepoURL, pmSyncTestRepoURL)
	}
}

func TestProcessPMCommit_withDueAndDates(t *testing.T) {
	setupTestDB(t)
	hash := "due123456789"
	content := "Issue with due\n\n" + `--- GitMsg: ext="pm"; type="issue"; state="open"; due="2025-12-31"; v="0.1.0" ---`
	insertTestCommit(t, hash, content)

	msg := protocol.ParseMessage(content)
	processPMCommit(git.Commit{Hash: hash, Timestamp: time.Now()}, msg, pmSyncTestRepoURL, pmSyncTestBranch)

	item := queryPMItem(t, hash)
	if !item.Due.Valid || item.Due.String != "2025-12-31" {
		t.Errorf("Due = %v, want 2025-12-31", item.Due)
	}
}

func TestProcessPMCommit_milestone(t *testing.T) {
	setupTestDB(t)
	hash := "mst123456789"
	content := "A milestone\n\n" + `--- GitMsg: ext="pm"; type="milestone"; state="open"; due="2025-06-30"; v="0.1.0" ---`
	insertTestCommit(t, hash, content)

	msg := protocol.ParseMessage(content)
	processPMCommit(git.Commit{Hash: hash, Timestamp: time.Now()}, msg, pmSyncTestRepoURL, pmSyncTestBranch)

	item := queryPMItem(t, hash)
	if item.Type != "milestone" {
		t.Errorf("Type = %q, want milestone", item.Type)
	}
}

func TestProcessPMCommit_sprint(t *testing.T) {
	setupTestDB(t)
	hash := "spt123456789"
	content := "A sprint\n\n" + `--- GitMsg: ext="pm"; type="sprint"; state="planned"; start="2025-10-01"; end="2025-10-14"; v="0.1.0" ---`
	insertTestCommit(t, hash, content)

	msg := protocol.ParseMessage(content)
	processPMCommit(git.Commit{Hash: hash, Timestamp: time.Now()}, msg, pmSyncTestRepoURL, pmSyncTestBranch)

	item := queryPMItem(t, hash)
	if item.Type != "sprint" {
		t.Errorf("Type = %q, want sprint", item.Type)
	}
	if !item.StartDate.Valid || item.StartDate.String != "2025-10-01" {
		t.Errorf("StartDate = %v, want 2025-10-01", item.StartDate)
	}
}

// Helper functions

func countPMItems(t *testing.T) int {
	t.Helper()
	count, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow(`SELECT COUNT(*) FROM pm_items`).Scan(&c)
		return c, err
	})
	if err != nil {
		t.Fatalf("countPMItems query error = %v", err)
	}
	return count
}

func queryPMItem(t *testing.T, hash string) PMItem {
	t.Helper()
	item, err := cache.QueryLocked(func(db *sql.DB) (PMItem, error) {
		var p PMItem
		err := db.QueryRow(`SELECT repo_url, hash, branch, type, state, assignees, due, start_date, end_date,
			milestone_repo_url, milestone_hash, milestone_branch,
			sprint_repo_url, sprint_hash, sprint_branch,
			parent_repo_url, parent_hash, parent_branch,
			root_repo_url, root_hash, root_branch, labels
			FROM pm_items WHERE repo_url = ? AND hash = ? AND branch = ?`,
			pmSyncTestRepoURL, hash, pmSyncTestBranch).Scan(
			&p.RepoURL, &p.Hash, &p.Branch, &p.Type, &p.State,
			&p.Assignees, &p.Due, &p.StartDate, &p.EndDate,
			&p.MilestoneRepoURL, &p.MilestoneHash, &p.MilestoneBranch,
			&p.SprintRepoURL, &p.SprintHash, &p.SprintBranch,
			&p.ParentRepoURL, &p.ParentHash, &p.ParentBranch,
			&p.RootRepoURL, &p.RootHash, &p.RootBranch, &p.Labels,
		)
		return p, err
	})
	if err != nil {
		t.Fatalf("queryPMItem error = %v", err)
	}
	return item
}
