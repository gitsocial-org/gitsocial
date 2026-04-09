// sync_test.go - Tests for review commit processing and workspace sync
package review

import (
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/protocol"
)

var repoTemplate string
var testCacheDir string

func TestMain(m *testing.M) {
	dir, _ := os.MkdirTemp("", "review-test-template-*")
	git.Init(dir, "main")
	git.ExecGit(dir, []string{"config", "user.email", "test@test.com"})
	git.ExecGit(dir, []string{"config", "user.name", "Test User"})
	git.CreateCommit(dir, git.CommitOptions{Message: "Initial commit", AllowEmpty: true})
	git.ExecGit(dir, []string{"remote", "add", "origin", "https://github.com/test/repo.git"})
	repoTemplate = dir

	cacheDir, _ := os.MkdirTemp("", "review-test-cache-*")
	cache.Open(cacheDir)
	testCacheDir = cacheDir

	code := m.Run()
	cache.Reset()
	os.RemoveAll(cacheDir)
	os.RemoveAll(dir)
	os.Exit(code)
}

func copyDir(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := os.MkdirAll(dstPath, 0o755); err != nil {
				return err
			}
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		srcFile, err := os.Open(srcPath)
		if err != nil {
			return err
		}
		dstFile, err := os.Create(dstPath)
		if err != nil {
			srcFile.Close()
			return err
		}
		_, err = io.Copy(dstFile, srcFile)
		srcFile.Close()
		dstFile.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

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

const reviewTestBranch = "gitmsg/review"
const reviewTestRepoURL = "https://github.com/test/repo"

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := copyDir(repoTemplate, dir); err != nil {
		t.Fatalf("copyDir() error = %v", err)
	}
	return dir
}

func insertReviewTestCommit(t *testing.T, repoURL, hash string) {
	t.Helper()
	if err := cache.InsertCommits([]cache.Commit{{
		Hash:        hash,
		RepoURL:     repoURL,
		Branch:      reviewTestBranch,
		AuthorName:  "Test User",
		AuthorEmail: "test@test.com",
		Message:     "test commit",
		Timestamp:   time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
}

func countReviewItems(t *testing.T) int {
	t.Helper()
	count, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow(`SELECT COUNT(*) FROM review_items`).Scan(&c)
		return c, err
	})
	if err != nil {
		t.Fatalf("countReviewItems query error = %v", err)
	}
	return count
}

func queryReviewItem(t *testing.T, repoURL, hash, branch string) ReviewItem { //nolint:unparam
	t.Helper()
	item, err := cache.QueryLocked(func(db *sql.DB) (ReviewItem, error) {
		var r ReviewItem
		var suggestion int
		err := db.QueryRow(`SELECT repo_url, hash, branch, type, state, base, head, closes, reviewers,
			pull_request_repo_url, pull_request_hash, pull_request_branch,
			commit_ref, file, old_line, new_line, old_line_end, new_line_end, review_state, suggestion
			FROM review_items WHERE repo_url = ? AND hash = ? AND branch = ?`,
			repoURL, hash, branch).Scan(
			&r.RepoURL, &r.Hash, &r.Branch, &r.Type, &r.State, &r.Base, &r.Head, &r.Closes, &r.Reviewers,
			&r.PullRequestRepoURL, &r.PullRequestHash, &r.PullRequestBranch,
			&r.CommitRef, &r.File, &r.OldLine, &r.NewLine, &r.OldLineEnd, &r.NewLineEnd, &r.ReviewStateField, &suggestion,
		)
		r.Suggestion = suggestion
		return r, err
	})
	if err != nil {
		t.Fatalf("queryReviewItem error = %v", err)
	}
	return item
}

func TestProcessReviewCommit_nilMessage(t *testing.T) {
	setupTestDB(t)
	gc := git.Commit{Hash: "abc123456789"}
	processReviewCommit(gc, nil, reviewTestRepoURL, reviewTestBranch)
	if count := countReviewItems(t); count != 0 {
		t.Errorf("expected 0 review_items, got %d", count)
	}
}

func TestProcessReviewCommit_wrongExtension(t *testing.T) {
	setupTestDB(t)
	msg := &protocol.Message{
		Content: "A social post",
		Header:  protocol.Header{Ext: "social", V: "0.1.0", Fields: map[string]string{"type": "post"}},
	}
	gc := git.Commit{Hash: "abc123456789"}
	processReviewCommit(gc, msg, reviewTestRepoURL, reviewTestBranch)
	if count := countReviewItems(t); count != 0 {
		t.Errorf("expected 0 review_items for wrong ext, got %d", count)
	}
}

func TestProcessReviewCommit_noType(t *testing.T) {
	setupTestDB(t)
	msg := &protocol.Message{
		Content: "No type field",
		Header:  protocol.Header{Ext: "review", V: "0.1.0", Fields: map[string]string{}},
	}
	gc := git.Commit{Hash: "abc123456789"}
	processReviewCommit(gc, msg, reviewTestRepoURL, reviewTestBranch)
	if count := countReviewItems(t); count != 0 {
		t.Errorf("expected 0 review_items for no type, got %d", count)
	}
}

func TestProcessReviewCommit_basicPR(t *testing.T) {
	setupTestDB(t)
	hash := "pr0123456789"
	content := "Add feature\n\n" + `GitMsg: ext="review"; type="pull-request"; state="open"; base="#branch:main"; head="#branch:feature"; closes="#commit:iss1,#commit:iss2"; reviewers="bob@test.com"; v="0.1.0"`
	insertReviewTestCommit(t, reviewTestRepoURL, hash)

	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: hash, Timestamp: time.Now()}
	processReviewCommit(gc, msg, reviewTestRepoURL, reviewTestBranch)

	item := queryReviewItem(t, reviewTestRepoURL, hash, reviewTestBranch)
	if item.Type != "pull-request" {
		t.Errorf("Type = %q, want pull-request", item.Type)
	}
	if !item.State.Valid || item.State.String != "open" {
		t.Errorf("State = %v, want open", item.State)
	}
	if !item.Base.Valid || item.Base.String != "#branch:main" {
		t.Errorf("Base = %v, want #branch:main", item.Base)
	}
	if !item.Head.Valid || item.Head.String != "#branch:feature" {
		t.Errorf("Head = %v, want #branch:feature", item.Head)
	}
	if !item.Closes.Valid || item.Closes.String != "#commit:iss1,#commit:iss2" {
		t.Errorf("Closes = %v", item.Closes)
	}
	if !item.Reviewers.Valid || item.Reviewers.String != "bob@test.com" {
		t.Errorf("Reviewers = %v", item.Reviewers)
	}
}

func TestProcessReviewCommit_feedback(t *testing.T) {
	setupTestDB(t)
	hash := "fb0123456789"
	content := "Looks good\n\n" + `GitMsg: ext="review"; type="feedback"; pull-request="#commit:ab0c12345678@gitmsg/review"; review-state="approved"; v="0.1.0"`
	insertReviewTestCommit(t, reviewTestRepoURL, hash)

	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: hash, Timestamp: time.Now()}
	processReviewCommit(gc, msg, reviewTestRepoURL, reviewTestBranch)

	item := queryReviewItem(t, reviewTestRepoURL, hash, reviewTestBranch)
	if item.Type != "feedback" {
		t.Errorf("Type = %q, want feedback", item.Type)
	}
	if !item.ReviewStateField.Valid || item.ReviewStateField.String != "approved" {
		t.Errorf("ReviewState = %v, want approved", item.ReviewStateField)
	}
	if !item.PullRequestHash.Valid || item.PullRequestHash.String != "ab0c12345678" {
		t.Errorf("PullRequestHash = %v, want ab0c12345678", item.PullRequestHash)
	}
	if !item.PullRequestRepoURL.Valid || item.PullRequestRepoURL.String != reviewTestRepoURL {
		t.Errorf("PullRequestRepoURL = %v, want %s", item.PullRequestRepoURL, reviewTestRepoURL)
	}
	if !item.PullRequestBranch.Valid || item.PullRequestBranch.String != reviewTestBranch {
		t.Errorf("PullRequestBranch = %v, want %s", item.PullRequestBranch, reviewTestBranch)
	}
}

func TestProcessReviewCommit_withEditsRef(t *testing.T) {
	setupTestDB(t)
	canonicalHash := "ca0123456789"
	editHash := "ed0123456789"
	insertReviewTestCommit(t, reviewTestRepoURL, canonicalHash)
	insertReviewTestCommit(t, reviewTestRepoURL, editHash)

	canonContent := "Original PR\n\n" + `GitMsg: ext="review"; type="pull-request"; state="open"; v="0.1.0"`
	msg := protocol.ParseMessage(canonContent)
	processReviewCommit(git.Commit{Hash: canonicalHash, Timestamp: time.Now()}, msg, reviewTestRepoURL, reviewTestBranch)

	editContent := "Updated PR\n\n" + `GitMsg: ext="review"; type="pull-request"; state="merged"; edits="#commit:` + canonicalHash + `@gitmsg/review"; v="0.1.0"`
	editMsg := protocol.ParseMessage(editContent)
	processReviewCommit(git.Commit{Hash: editHash, Timestamp: time.Now()}, editMsg, reviewTestRepoURL, reviewTestBranch)

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

func TestProcessReviewCommit_crossRepoEdit(t *testing.T) {
	setupTestDB(t)
	canonHash := "ce0123456789"
	editHash := "ce0234567890"
	insertReviewTestCommit(t, reviewTestRepoURL, canonHash)
	insertReviewTestCommit(t, reviewTestRepoURL, editHash)

	editContent := "Edited\n\n" + `GitMsg: ext="review"; type="pull-request"; edits="https://github.com/other/repo#commit:` + canonHash + `@gitmsg/review"; v="0.1.0"`
	msg := protocol.ParseMessage(editContent)
	processReviewCommit(git.Commit{Hash: editHash, Timestamp: time.Now()}, msg, reviewTestRepoURL, reviewTestBranch)
}

func TestProcessReviewCommit_crossRepoHeadQualification(t *testing.T) {
	setupTestDB(t)
	hash := "xr0123456789"
	content := "Cross-repo PR\n\n" + `GitMsg: ext="review"; type="pull-request"; state="open"; base="https://github.com/upstream/repo#branch:main"; head="#branch:feature"; v="0.1.0"`
	insertReviewTestCommit(t, reviewTestRepoURL, hash)

	msg := protocol.ParseMessage(content)
	processReviewCommit(git.Commit{Hash: hash, Timestamp: time.Now()}, msg, reviewTestRepoURL, reviewTestBranch)

	item := queryReviewItem(t, reviewTestRepoURL, hash, reviewTestBranch)
	if !item.Head.Valid || item.Head.String == "#branch:feature" {
		t.Errorf("Head should be qualified with repo URL, got %v", item.Head)
	}
}

func TestProcessReviewCommit_lineFields(t *testing.T) {
	setupTestDB(t)
	hash := "ln0123456789"
	content := "Inline comment\n\n" + `GitMsg: ext="review"; type="feedback"; commit="abc123"; file="main.go"; old-line="10"; new-line="15"; old-line-end="12"; new-line-end="17"; v="0.1.0"`
	insertReviewTestCommit(t, reviewTestRepoURL, hash)

	msg := protocol.ParseMessage(content)
	processReviewCommit(git.Commit{Hash: hash, Timestamp: time.Now()}, msg, reviewTestRepoURL, reviewTestBranch)

	item := queryReviewItem(t, reviewTestRepoURL, hash, reviewTestBranch)
	if !item.OldLine.Valid || item.OldLine.Int64 != 10 {
		t.Errorf("OldLine = %v, want 10", item.OldLine)
	}
	if !item.NewLine.Valid || item.NewLine.Int64 != 15 {
		t.Errorf("NewLine = %v, want 15", item.NewLine)
	}
	if !item.OldLineEnd.Valid || item.OldLineEnd.Int64 != 12 {
		t.Errorf("OldLineEnd = %v, want 12", item.OldLineEnd)
	}
	if !item.NewLineEnd.Valid || item.NewLineEnd.Int64 != 17 {
		t.Errorf("NewLineEnd = %v, want 17", item.NewLineEnd)
	}
	if !item.CommitRef.Valid || item.CommitRef.String != "abc123" {
		t.Errorf("CommitRef = %v, want abc123", item.CommitRef)
	}
	if !item.File.Valid || item.File.String != "main.go" {
		t.Errorf("File = %v, want main.go", item.File)
	}
}

func TestProcessReviewCommit_suggestion(t *testing.T) {
	setupTestDB(t)
	hash := "sg0123456789"
	content := "Use this instead\n\n" + `GitMsg: ext="review"; type="feedback"; suggestion="true"; v="0.1.0"`
	insertReviewTestCommit(t, reviewTestRepoURL, hash)

	msg := protocol.ParseMessage(content)
	processReviewCommit(git.Commit{Hash: hash, Timestamp: time.Now()}, msg, reviewTestRepoURL, reviewTestBranch)

	item := queryReviewItem(t, reviewTestRepoURL, hash, reviewTestBranch)
	if item.Suggestion != 1 {
		t.Errorf("Suggestion = %d, want 1", item.Suggestion)
	}
}

func TestProcessReviewCommit_retraction(t *testing.T) {
	setupTestDB(t)
	canonHash := "ae0123456789"
	retractHash := "ae0234567890"
	insertReviewTestCommit(t, reviewTestRepoURL, canonHash)
	insertReviewTestCommit(t, reviewTestRepoURL, retractHash)

	canonContent := "PR\n\n" + `GitMsg: ext="review"; type="pull-request"; state="open"; v="0.1.0"`
	msg := protocol.ParseMessage(canonContent)
	processReviewCommit(git.Commit{Hash: canonHash, Timestamp: time.Now()}, msg, reviewTestRepoURL, reviewTestBranch)

	retractContent := `GitMsg: ext="review"; type="pull-request"; edits="#commit:` + canonHash + `@gitmsg/review"; retracted="true"; v="0.1.0"`
	retractMsg := protocol.ParseMessage(retractContent)
	processReviewCommit(git.Commit{Hash: retractHash, Timestamp: time.Now()}, retractMsg, reviewTestRepoURL, reviewTestBranch)

	count, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var c int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_commits_version WHERE edit_hash = ? AND is_retracted = 1`,
			retractHash).Scan(&c)
		return c, err
	})
	if err != nil {
		t.Fatalf("query error = %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 retraction version row, got %d", count)
	}
}

func TestProcessReviewCommit_dbError(t *testing.T) {
	cache.Reset()
	t.Cleanup(func() {
		cache.Reset()
		cache.Open(testCacheDir)
	})

	content := "PR\n\n" + `GitMsg: ext="review"; type="pull-request"; v="0.1.0"`
	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: "abc123456789", Timestamp: time.Now()}
	processReviewCommit(gc, msg, reviewTestRepoURL, reviewTestBranch)
}

func TestSyncWorkspace(t *testing.T) {
	t.Parallel()

	t.Run("noBranch", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		if err := SyncWorkspaceToCache(dir); err != nil {
			t.Fatalf("SyncWorkspaceToCache() error = %v", err)
		}
	})

	t.Run("withCommits", func(t *testing.T) {
		t.Parallel()
		dir := initTestRepo(t)
		git.CreateCommitOnBranch(dir, "gitmsg/review", "Add feature\n\n"+`GitMsg: ext="review"; type="pull-request"; state="open"; base="#branch:main"; head="#branch:feature"; v="0.1.0"`)
		git.CreateCommitOnBranch(dir, "gitmsg/review", "Fix bug\n\n"+`GitMsg: ext="review"; type="pull-request"; state="open"; base="#branch:main"; head="#branch:bugfix"; v="0.1.0"`)

		if err := SyncWorkspaceToCache(dir); err != nil {
			t.Fatalf("SyncWorkspaceToCache() error = %v", err)
		}

		res := GetPullRequests("https://github.com/test/repo", "gitmsg/review", nil, "", 10)
		if !res.Success {
			t.Fatalf("GetPullRequests() failed: %s", res.Error.Message)
		}
		if len(res.Data) < 2 {
			t.Errorf("expected at least 2 PRs, got %d", len(res.Data))
		}
	})
}

func TestSyncWorkspaceToCache_cacheError(t *testing.T) {
	dir := initTestRepo(t)
	git.CreateCommitOnBranch(dir, "gitmsg/review", "PR\n\n"+`GitMsg: ext="review"; type="pull-request"; v="0.1.0"`)

	cache.Reset()
	t.Cleanup(func() {
		cache.Reset()
		cache.Open(testCacheDir)
	})

	err := SyncWorkspaceToCache(dir)
	if err == nil {
		t.Error("should fail with cache not open")
	}
}

func TestProcessReviewCommit_feedbackWithNoBranchInPRRef(t *testing.T) {
	setupTestDB(t)
	hash := "fb1234567890"
	content := "Comment\n\n" + `GitMsg: ext="review"; type="feedback"; pull-request="#commit:ab0c12345678"; v="0.1.0"`
	insertReviewTestCommit(t, reviewTestRepoURL, hash)

	msg := protocol.ParseMessage(content)
	gc := git.Commit{Hash: hash, Timestamp: time.Now()}
	processReviewCommit(gc, msg, reviewTestRepoURL, reviewTestBranch)

	item := queryReviewItem(t, reviewTestRepoURL, hash, reviewTestBranch)
	if !item.PullRequestBranch.Valid || item.PullRequestBranch.String != reviewTestBranch {
		t.Errorf("PullRequestBranch = %v, want %s (should default to commit branch)", item.PullRequestBranch, reviewTestBranch)
	}
}

func TestBoolToInt(t *testing.T) {
	if boolToInt(true) != 1 {
		t.Error("boolToInt(true) should be 1")
	}
	if boolToInt(false) != 0 {
		t.Error("boolToInt(false) should be 0")
	}
}

func TestParseIntValues(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"42", 42},
		{"0", 0},
		{"123", 123},
		{"abc", 0},
		{"12x3", 0},
		{"", 0},
	}
	for _, tt := range tests {
		if got := parseInt(tt.input); got != tt.want {
			t.Errorf("parseInt(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
