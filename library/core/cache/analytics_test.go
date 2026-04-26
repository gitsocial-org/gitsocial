// analytics_test.go - Tests for analytics queries
package cache

import (
	"database/sql"
	"testing"
	"time"
)

const pmSchemaForTest = `
CREATE TABLE IF NOT EXISTS pm_items (
    repo_url TEXT NOT NULL, hash TEXT NOT NULL, branch TEXT NOT NULL,
    type TEXT NOT NULL, state TEXT NOT NULL DEFAULT 'open',
    assignees TEXT, due TEXT, start_date TEXT, end_date TEXT,
    milestone_repo_url TEXT, milestone_hash TEXT, milestone_branch TEXT,
    sprint_repo_url TEXT, sprint_hash TEXT, sprint_branch TEXT,
    parent_repo_url TEXT, parent_hash TEXT, parent_branch TEXT,
    root_repo_url TEXT, root_hash TEXT, root_branch TEXT, labels TEXT,
    PRIMARY KEY (repo_url, hash, branch)
);
`

const releaseSchemaForTest = `
CREATE TABLE IF NOT EXISTS release_items (
    repo_url TEXT NOT NULL, hash TEXT NOT NULL, branch TEXT NOT NULL,
    tag TEXT, version TEXT, prerelease INTEGER DEFAULT 0,
    artifacts TEXT, artifact_url TEXT, checksums TEXT, signed_by TEXT,
    PRIMARY KEY (repo_url, hash, branch)
);
`

const reviewSchemaForTest = `
CREATE TABLE IF NOT EXISTS review_items (
    repo_url TEXT NOT NULL, hash TEXT NOT NULL, branch TEXT NOT NULL,
    type TEXT NOT NULL, state TEXT,
    base TEXT, base_tip TEXT, head TEXT, head_tip TEXT,
    closes TEXT, reviewers TEXT,
    pull_request_repo_url TEXT, pull_request_hash TEXT, pull_request_branch TEXT,
    commit_ref TEXT, file TEXT,
    old_line INTEGER, new_line INTEGER, old_line_end INTEGER, new_line_end INTEGER,
    review_state TEXT, suggestion INTEGER DEFAULT 0,
    PRIMARY KEY (repo_url, hash, branch)
);
`

func setupTestDBWithAllSchemas(t *testing.T) {
	t.Helper()
	schemaMu.Lock()
	extensionSchemas["social"] = socialSchemaForTest
	extensionSchemas["pm"] = pmSchemaForTest
	extensionSchemas["release"] = releaseSchemaForTest
	extensionSchemas["review"] = reviewSchemaForTest
	schemaMu.Unlock()
	Reset()
	dir := t.TempDir()
	if err := Open(dir); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		Reset()
		schemaMu.Lock()
		delete(extensionSchemas, "social")
		delete(extensionSchemas, "pm")
		delete(extensionSchemas, "release")
		delete(extensionSchemas, "review")
		schemaMu.Unlock()
	})
}

func insertTestCommit(t *testing.T, repoURL, hash string, ts time.Time) {
	t.Helper()
	InsertCommits([]Commit{{
		Hash: hash, RepoURL: repoURL, Branch: "main",
		AuthorName: "Test User", AuthorEmail: "test@test.com",
		Message: "test commit", Timestamp: ts,
	}})
}

func TestRepoFilter(t *testing.T) {
	tests := []struct {
		name    string
		alias   string
		repoURL string
		wantSQL string
		wantLen int
	}{
		{"empty repoURL", "v", "", "", 0},
		{"with alias", "v", "https://github.com/u/r", " AND v.repo_url = ?", 1},
		{"no alias", "", "https://github.com/u/r", " AND repo_url = ?", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clause, args := repoFilter(tt.alias, tt.repoURL)
			if clause != tt.wantSQL {
				t.Errorf("clause = %q, want %q", clause, tt.wantSQL)
			}
			if len(args) != tt.wantLen {
				t.Errorf("len(args) = %d, want %d", len(args), tt.wantLen)
			}
		})
	}
}

func TestListFilter(t *testing.T) {
	tests := []struct {
		name    string
		alias   string
		urls    []string
		wantLen int
		wantNil bool
	}{
		{"empty urls", "v", nil, 0, true},
		{"single url", "v", []string{"url1"}, 1, false},
		{"multiple urls", "", []string{"url1", "url2", "url3"}, 3, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clause, args := listFilter(tt.alias, tt.urls)
			if tt.wantNil {
				if clause != "" {
					t.Errorf("clause = %q, want empty", clause)
				}
				return
			}
			if len(args) != tt.wantLen {
				t.Errorf("len(args) = %d, want %d", len(args), tt.wantLen)
			}
			if clause == "" {
				t.Error("clause should not be empty")
			}
		})
	}
}

func TestGetAnalytics_dbNotOpen(t *testing.T) {
	Reset()
	data, err := GetAnalytics("")
	if err != nil {
		t.Fatalf("GetAnalytics() error = %v", err)
	}
	if data == nil {
		t.Fatal("GetAnalytics() returned nil")
	}
}

func TestGetAnalytics_empty(t *testing.T) {
	setupTestDBWithAllSchemas(t)

	data, err := GetAnalytics("")
	if err != nil {
		t.Fatalf("GetAnalytics() error = %v", err)
	}
	if data.TotalCommits != 0 {
		t.Errorf("TotalCommits = %d, want 0", data.TotalCommits)
	}
}

func TestGetAnalytics_withCommits(t *testing.T) {
	setupTestDBWithAllSchemas(t)

	now := time.Now().UTC()
	repoURL := "https://github.com/user/repo"
	insertTestCommit(t, repoURL, "commit1_hash", now.Add(-1*time.Hour))
	insertTestCommit(t, repoURL, "commit2_hash", now.Add(-2*time.Hour))
	insertTestCommit(t, repoURL, "commit3_hash", now.Add(-3*time.Hour))

	// Insert a repo to cover TrackedRepos
	InsertRepository(Repository{URL: repoURL, Branch: "main", StoragePath: "/tmp/test"})

	data, err := GetAnalytics("")
	if err != nil {
		t.Fatalf("GetAnalytics() error = %v", err)
	}
	if data.TotalCommits != 3 {
		t.Errorf("TotalCommits = %d, want 3", data.TotalCommits)
	}
	if data.TotalContributors != 1 {
		t.Errorf("TotalContributors = %d, want 1", data.TotalContributors)
	}
	if len(data.Contributors) != 1 {
		t.Errorf("len(Contributors) = %d, want 1", len(data.Contributors))
	}
	if len(data.RepoActivity) != 1 {
		t.Errorf("len(RepoActivity) = %d, want 1", len(data.RepoActivity))
	}
	if data.TrackedRepos != 1 {
		t.Errorf("TrackedRepos = %d, want 1", data.TrackedRepos)
	}
	if data.ActiveRepos != 1 {
		t.Errorf("ActiveRepos = %d, want 1", data.ActiveRepos)
	}
}

func TestGetAnalytics_scopedToRepo(t *testing.T) {
	setupTestDBWithAllSchemas(t)

	now := time.Now().UTC()
	insertTestCommit(t, "https://github.com/user/repo1", "commit1_hash", now.Add(-1*time.Hour))
	insertTestCommit(t, "https://github.com/user/repo2", "commit2_hash", now.Add(-2*time.Hour))

	data, err := GetAnalytics("https://github.com/user/repo1")
	if err != nil {
		t.Fatalf("GetAnalytics() error = %v", err)
	}
	if data.TotalCommits != 1 {
		t.Errorf("TotalCommits = %d, want 1", data.TotalCommits)
	}
	if data.RepoURL != "https://github.com/user/repo1" {
		t.Errorf("RepoURL = %q", data.RepoURL)
	}
}

func TestGetAnalytics_withSocialData(t *testing.T) {
	setupTestDBWithAllSchemas(t)

	now := time.Now().UTC()
	repoURL := "https://github.com/user/repo"
	insertTestCommit(t, repoURL, "post1_hash_", now.Add(-1*time.Hour))
	insertTestCommit(t, repoURL, "post2_hash_", now.Add(-2*time.Hour))
	insertTestCommit(t, repoURL, "comment_hsh", now.Add(-3*time.Hour))

	ExecLocked(func(db *sql.DB) error {
		db.Exec(`INSERT INTO social_items (repo_url, hash, branch, type) VALUES (?, ?, 'main', 'post')`, repoURL, "post1_hash_")
		db.Exec(`INSERT INTO social_items (repo_url, hash, branch, type) VALUES (?, ?, 'main', 'post')`, repoURL, "post2_hash_")
		db.Exec(`INSERT INTO social_items (repo_url, hash, branch, type) VALUES (?, ?, 'main', 'comment')`, repoURL, "comment_hsh")
		return nil
	})

	data, err := GetAnalytics(repoURL)
	if err != nil {
		t.Fatalf("GetAnalytics() error = %v", err)
	}
	if data.Social == nil {
		t.Fatal("Social analytics should not be nil")
	}
	if data.Social.TotalPosts != 2 {
		t.Errorf("TotalPosts = %d, want 2", data.Social.TotalPosts)
	}
	if data.Social.TotalComments != 1 {
		t.Errorf("TotalComments = %d, want 1", data.Social.TotalComments)
	}
}

func TestGetAnalytics_withPMData(t *testing.T) {
	setupTestDBWithAllSchemas(t)

	now := time.Now().UTC()
	repoURL := "https://github.com/user/repo"
	insertTestCommit(t, repoURL, "issue1_hash", now.Add(-1*time.Hour))
	insertTestCommit(t, repoURL, "issue2_hash", now.Add(-2*time.Hour))
	insertTestCommit(t, repoURL, "milestone_h", now.Add(-3*time.Hour))

	ExecLocked(func(db *sql.DB) error {
		db.Exec(`INSERT INTO pm_items (repo_url, hash, branch, type, state) VALUES (?, ?, 'main', 'issue', 'open')`, repoURL, "issue1_hash")
		db.Exec(`INSERT INTO pm_items (repo_url, hash, branch, type, state) VALUES (?, ?, 'main', 'issue', 'closed')`, repoURL, "issue2_hash")
		db.Exec(`INSERT INTO pm_items (repo_url, hash, branch, type, state, due) VALUES (?, ?, 'main', 'milestone', 'open', '2026-03-01')`, repoURL, "milestone_h")
		return nil
	})

	data, err := GetAnalytics(repoURL)
	if err != nil {
		t.Fatalf("GetAnalytics() error = %v", err)
	}
	if data.PM == nil {
		t.Fatal("PM analytics should not be nil")
	}
	if data.PM.OpenIssues != 1 {
		t.Errorf("OpenIssues = %d, want 1", data.PM.OpenIssues)
	}
	if data.PM.ClosedIssues != 1 {
		t.Errorf("ClosedIssues = %d, want 1", data.PM.ClosedIssues)
	}
}

func TestGetAnalytics_withReleaseData(t *testing.T) {
	setupTestDBWithAllSchemas(t)

	now := time.Now().UTC()
	repoURL := "https://github.com/user/repo"
	insertTestCommit(t, repoURL, "release_hsh", now.Add(-1*time.Hour))

	ExecLocked(func(db *sql.DB) error {
		db.Exec(`INSERT INTO release_items (repo_url, hash, branch, tag, version) VALUES (?, ?, 'main', 'v1.0.0', '1.0.0')`, repoURL, "release_hsh")
		return nil
	})

	data, err := GetAnalytics(repoURL)
	if err != nil {
		t.Fatalf("GetAnalytics() error = %v", err)
	}
	if data.Release == nil {
		t.Fatal("Release analytics should not be nil")
	}
	if len(data.Release.Recent) != 1 {
		t.Errorf("len(Recent) = %d, want 1", len(data.Release.Recent))
	}
	if data.Release.Total != 1 {
		t.Errorf("Total = %d, want 1", data.Release.Total)
	}
}

func TestGetNetworkAnalytics_dbNotOpen(t *testing.T) {
	Reset()
	na := GetNetworkAnalytics("/tmp/test")
	if na == nil {
		t.Fatal("GetNetworkAnalytics() returned nil")
	}
}

func TestGetNetworkAnalytics_empty(t *testing.T) {
	setupTestDBWithAllSchemas(t)

	na := GetNetworkAnalytics("/tmp/test")
	if na == nil {
		t.Fatal("GetNetworkAnalytics() returned nil")
	}
	if len(na.Repos) != 0 {
		t.Errorf("len(Repos) = %d, want 0", len(na.Repos))
	}
}

func TestGetNetworkAnalytics_withData(t *testing.T) {
	setupTestDBWithAllSchemas(t)

	now := time.Now().UTC()
	repoURL := "https://github.com/user/repo"
	insertTestCommit(t, repoURL, "commit1_net", now.Add(-1*time.Hour))

	// Create a list with the repo
	InsertList(CachedList{ID: "list-1", Name: "following", Workdir: "/tmp/test"})
	AddRepositoryToList("list-1", repoURL, "main")

	na := GetNetworkAnalytics("/tmp/test")
	if na == nil {
		t.Fatal("GetNetworkAnalytics() returned nil")
	}
	if na.TrackedRepos != 1 {
		t.Errorf("TrackedRepos = %d, want 1", na.TrackedRepos)
	}
	if len(na.Repos) != 1 {
		t.Errorf("len(Repos) = %d, want 1", len(na.Repos))
	}
}

func TestGetNetworkAnalytics_withSocialData(t *testing.T) {
	setupTestDBWithAllSchemas(t)

	now := time.Now().UTC()
	repoURL := "https://github.com/user/repo"
	insertTestCommit(t, repoURL, "social_net1", now.Add(-1*time.Hour))

	ExecLocked(func(db *sql.DB) error {
		db.Exec(`INSERT INTO social_items (repo_url, hash, branch, type) VALUES (?, ?, 'main', 'post')`, repoURL, "social_net1")
		return nil
	})

	InsertList(CachedList{ID: "list-1", Name: "following", Workdir: "/tmp/test"})
	AddRepositoryToList("list-1", repoURL, "main")

	na := GetNetworkAnalytics("/tmp/test")
	if na.Social == nil {
		t.Fatal("Social should not be nil")
	}
	if na.Social.TotalPosts != 1 {
		t.Errorf("TotalPosts = %d, want 1", na.Social.TotalPosts)
	}
}

func TestGetNetworkAnalytics_withPMData(t *testing.T) {
	setupTestDBWithAllSchemas(t)

	now := time.Now().UTC()
	repoURL := "https://github.com/user/repo"
	insertTestCommit(t, repoURL, "pm_net_hash", now.Add(-1*time.Hour))

	ExecLocked(func(db *sql.DB) error {
		db.Exec(`INSERT INTO pm_items (repo_url, hash, branch, type, state) VALUES (?, ?, 'main', 'issue', 'open')`, repoURL, "pm_net_hash")
		return nil
	})

	InsertList(CachedList{ID: "list-1", Name: "following", Workdir: "/tmp/test"})
	AddRepositoryToList("list-1", repoURL, "main")

	na := GetNetworkAnalytics("/tmp/test")
	if na.PM == nil {
		t.Fatal("PM should not be nil")
	}
	if na.PM.TotalOpen != 1 {
		t.Errorf("TotalOpen = %d, want 1", na.PM.TotalOpen)
	}
}

func TestGetNetworkAnalytics_withReleaseData(t *testing.T) {
	setupTestDBWithAllSchemas(t)

	now := time.Now().UTC()
	repoURL := "https://github.com/user/repo"
	insertTestCommit(t, repoURL, "rel_net_hsh", now.Add(-1*time.Hour))

	ExecLocked(func(db *sql.DB) error {
		db.Exec(`INSERT INTO release_items (repo_url, hash, branch, tag, version) VALUES (?, ?, 'main', 'v1.0.0', '1.0.0')`, repoURL, "rel_net_hsh")
		return nil
	})

	InsertList(CachedList{ID: "list-1", Name: "following", Workdir: "/tmp/test"})
	AddRepositoryToList("list-1", repoURL, "main")

	na := GetNetworkAnalytics("/tmp/test")
	if na.Release == nil {
		t.Fatal("Release should not be nil")
	}
	if len(na.Release.Recent) != 1 {
		t.Errorf("len(Recent) = %d, want 1", len(na.Release.Recent))
	}
}

func TestRepoHasExtRows(t *testing.T) {
	setupTestDBWithAllSchemas(t)

	if repoHasExtRows("social_items", "") {
		t.Error("should be false for empty table")
	}

	now := time.Now().UTC()
	repoURL := "https://github.com/user/repo"
	insertTestCommit(t, repoURL, "ext_row_hsh", now)

	ExecLocked(func(db *sql.DB) error {
		db.Exec(`INSERT INTO social_items (repo_url, hash, branch, type) VALUES (?, ?, 'main', 'post')`, repoURL, "ext_row_hsh")
		return nil
	})

	if !repoHasExtRows("social_items", "") {
		t.Error("should be true after insert (unscoped)")
	}
	if !repoHasExtRows("social_items", repoURL) {
		t.Error("should be true after insert (scoped)")
	}
	if repoHasExtRows("social_items", "https://github.com/other/repo") {
		t.Error("should be false for different repo")
	}
}

func TestGetAnalytics_releasePerMonth(t *testing.T) {
	setupTestDBWithAllSchemas(t)

	repoURL := "https://github.com/user/repo"
	// Insert two releases with timestamps 90 days apart so months > 0
	ts1 := time.Now().UTC().Add(-1 * time.Hour)
	ts2 := time.Now().UTC().Add(-90 * 24 * time.Hour)
	insertTestCommit(t, repoURL, "release_1_h", ts1)
	insertTestCommit(t, repoURL, "release_2_h", ts2)

	ExecLocked(func(db *sql.DB) error {
		db.Exec(`INSERT INTO release_items (repo_url, hash, branch, tag, version) VALUES (?, ?, 'main', 'v2.0.0', '2.0.0')`, repoURL, "release_1_h")
		db.Exec(`INSERT INTO release_items (repo_url, hash, branch, tag, version) VALUES (?, ?, 'main', 'v1.0.0', '1.0.0')`, repoURL, "release_2_h")
		return nil
	})

	data, err := GetAnalytics(repoURL)
	if err != nil {
		t.Fatalf("GetAnalytics() error = %v", err)
	}
	if data.Release == nil {
		t.Fatal("Release analytics should not be nil")
	}
	if data.Release.Total != 2 {
		t.Errorf("Total = %d, want 2", data.Release.Total)
	}
	if data.Release.PerMonth <= 0 {
		t.Errorf("PerMonth = %f, want > 0", data.Release.PerMonth)
	}
}

func TestGetListRepoURLs_queryError(t *testing.T) {
	setupTestDB(t)
	ExecLocked(func(db *sql.DB) error { _, err := db.Exec("DROP TABLE core_list_repositories"); return err })
	urls := getListRepoURLs("/ws")
	if urls != nil {
		t.Errorf("getListRepoURLs() should return nil when table is dropped, got %v", urls)
	}
}

func TestGetListRepoURLs_dbNotOpen(t *testing.T) {
	Reset()
	urls := getListRepoURLs("/tmp/test")
	if urls != nil {
		t.Errorf("getListRepoURLs() should return nil when db not open, got %v", urls)
	}
}

func TestRepoHasExtRows_dbNotOpen(t *testing.T) {
	Reset()
	if repoHasExtRows("social_items", "") {
		t.Error("should be false when db not open")
	}
}

func TestGetAnalytics_excludesStale(t *testing.T) {
	setupTestDBWithAllSchemas(t)

	now := time.Now().UTC()
	repoURL := "https://github.com/user/repo"
	insertTestCommit(t, repoURL, "fresh_hash_", now.Add(-1*time.Hour))
	insertTestCommit(t, repoURL, "stale_hash_", now.Add(-2*time.Hour))

	// Mark one commit as stale (update both source and materialized tables)
	ExecLocked(func(db *sql.DB) error {
		db.Exec(`UPDATE core_commits SET stale_since = ? WHERE hash = ?`,
			now.Format(time.RFC3339), "stale_hash_")
		db.Exec(`UPDATE core_commits SET stale_since = ? WHERE hash = ?`,
			now.Format(time.RFC3339), "stale_hash_")
		return nil
	})

	data, err := GetAnalytics("")
	if err != nil {
		t.Fatalf("GetAnalytics() error = %v", err)
	}
	if data.TotalCommits != 1 {
		t.Errorf("TotalCommits = %d, want 1 (stale commit should be excluded)", data.TotalCommits)
	}
}

func TestGetAnalytics_excludesEditCommits(t *testing.T) {
	setupTestDBWithAllSchemas(t)

	now := time.Now().UTC()
	repoURL := "https://github.com/user/repo"
	// Insert canonical commit
	insertTestCommit(t, repoURL, "canonical__", now.Add(-2*time.Hour))
	// Insert edit commit
	insertTestCommit(t, repoURL, "edit_commit", now.Add(-1*time.Hour))

	// Create version record making edit_commit an edit of canonical__
	ExecLocked(func(db *sql.DB) error {
		db.Exec(`INSERT INTO core_commits_version
			(edit_repo_url, edit_hash, edit_branch, canonical_repo_url, canonical_hash, canonical_branch, is_retracted)
			VALUES (?, ?, 'main', ?, ?, 'main', 0)`,
			repoURL, "edit_commit", repoURL, "canonical__")
		db.Exec(`UPDATE core_commits SET is_edit_commit = 1 WHERE hash = ?`, "edit_commit")
		db.Exec(`UPDATE core_commits SET has_edits = 1 WHERE hash = ?`, "canonical__")
		return nil
	})

	data, err := GetAnalytics("")
	if err != nil {
		t.Fatalf("GetAnalytics() error = %v", err)
	}
	// Only the canonical commit should be counted (edit commit is excluded)
	if data.TotalCommits != 1 {
		t.Errorf("TotalCommits = %d, want 1 (edit commit should be excluded)", data.TotalCommits)
	}
}

func TestGetAnalytics_excludesRetracted(t *testing.T) {
	setupTestDBWithAllSchemas(t)

	now := time.Now().UTC()
	repoURL := "https://github.com/user/repo"
	insertTestCommit(t, repoURL, "normal_hsh_", now.Add(-2*time.Hour))
	insertTestCommit(t, repoURL, "retracted__", now.Add(-3*time.Hour))
	// Insert the retraction edit commit
	insertTestCommit(t, repoURL, "retract_edt", now.Add(-1*time.Hour))

	// Create version record making retracted__ retracted via retract_edt
	ExecLocked(func(db *sql.DB) error {
		db.Exec(`INSERT INTO core_commits_version
			(edit_repo_url, edit_hash, edit_branch, canonical_repo_url, canonical_hash, canonical_branch, is_retracted)
			VALUES (?, ?, 'main', ?, ?, 'main', 1)`,
			repoURL, "retract_edt", repoURL, "retracted__")
		db.Exec(`UPDATE core_commits SET is_edit_commit = 1 WHERE hash = ?`, "retract_edt")
		db.Exec(`UPDATE core_commits SET is_retracted = 1, has_edits = 1 WHERE hash = ?`, "retracted__")
		return nil
	})

	data, err := GetAnalytics("")
	if err != nil {
		t.Fatalf("GetAnalytics() error = %v", err)
	}
	// normal + retracted canonical (now is_retracted=1) + retract_edt (is_edit_commit=1)
	// Only normal_hsh_ should be counted
	if data.TotalCommits != 1 {
		t.Errorf("TotalCommits = %d, want 1 (retracted and edit commits should be excluded)", data.TotalCommits)
	}
}

func TestGetAnalytics_originAuthor(t *testing.T) {
	setupTestDBWithAllSchemas(t)

	now := time.Now().UTC()
	repoURL := "https://github.com/user/repo"
	// Insert a commit with origin-author fields in the GitMsg header
	msg := "Imported post\n\nGitMsg: ext=\"social\" v=\"0.1.0\" type=\"post\" origin-author-name=\"Original Author\" origin-author-email=\"original@example.com\""
	InsertCommits([]Commit{{
		Hash: "origin_hash", RepoURL: repoURL, Branch: "main",
		AuthorName: "Importer Bot", AuthorEmail: "bot@importer.com",
		Message: msg, Timestamp: now.Add(-1 * time.Hour),
	}})

	data, err := GetAnalytics(repoURL)
	if err != nil {
		t.Fatalf("GetAnalytics() error = %v", err)
	}
	if len(data.Contributors) != 1 {
		t.Fatalf("len(Contributors) = %d, want 1", len(data.Contributors))
	}
	// The resolved view should COALESCE origin author over git author
	if data.Contributors[0].Name != "Original Author" {
		t.Errorf("Contributor name = %q, want %q", data.Contributors[0].Name, "Original Author")
	}
	if data.Contributors[0].Email != "original@example.com" {
		t.Errorf("Contributor email = %q, want %q", data.Contributors[0].Email, "original@example.com")
	}
}

func TestGetAnalytics_withReviewData(t *testing.T) {
	setupTestDBWithAllSchemas(t)

	now := time.Now().UTC()
	repoURL := "https://github.com/user/repo"
	insertTestCommit(t, repoURL, "pr_open_hsh", now.Add(-1*time.Hour))
	insertTestCommit(t, repoURL, "pr_mrgd_hsh", now.Add(-2*time.Hour))
	insertTestCommit(t, repoURL, "feedback_hh", now.Add(-3*time.Hour))

	ExecLocked(func(db *sql.DB) error {
		db.Exec(`INSERT INTO review_items (repo_url, hash, branch, type, state) VALUES (?, ?, 'main', 'pull-request', 'open')`, repoURL, "pr_open_hsh")
		db.Exec(`INSERT INTO review_items (repo_url, hash, branch, type, state) VALUES (?, ?, 'main', 'pull-request', 'merged')`, repoURL, "pr_mrgd_hsh")
		db.Exec(`INSERT INTO review_items (repo_url, hash, branch, type, state, pull_request_repo_url, pull_request_hash, pull_request_branch) VALUES (?, ?, 'main', 'feedback', 'approved', ?, ?, 'main')`,
			repoURL, "feedback_hh", repoURL, "pr_open_hsh")
		return nil
	})

	data, err := GetAnalytics(repoURL)
	if err != nil {
		t.Fatalf("GetAnalytics() error = %v", err)
	}
	if data.Review == nil {
		t.Fatal("Review analytics should not be nil")
	}
	if data.Review.OpenPRs != 1 {
		t.Errorf("OpenPRs = %d, want 1", data.Review.OpenPRs)
	}
	if data.Review.MergedPRs != 1 {
		t.Errorf("MergedPRs = %d, want 1", data.Review.MergedPRs)
	}
	if data.Review.TotalFeedback != 1 {
		t.Errorf("TotalFeedback = %d, want 1", data.Review.TotalFeedback)
	}
}
