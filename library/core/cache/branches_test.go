// branches_test.go - The branch list comes from the live fetched rows of the cache alone
package cache

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestGetRepositoryBranches checks that the list reads the cache with no storage, leaves out virtual, stale, edit and state-ref rows, and orders code branches before gitmsg/* by last commit time.
func TestGetRepositoryBranches(t *testing.T) {
	Reset()
	dir := t.TempDir()
	if err := Open(dir); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { Reset() })
	repo := "https://github.com/user/repo"
	at := func(hour int) time.Time { return time.Date(2025, 10, 21, hour, 0, 0, 0, time.UTC) }
	if err := InsertCommits([]Commit{
		{Hash: "main00000001", RepoURL: repo, Branch: "main", Message: "Add the index", Timestamp: at(1)},
		{Hash: "main00000002", RepoURL: repo, Branch: "main", Message: "Add a page", Timestamp: at(4)},
		{Hash: "feat00000001", RepoURL: repo, Branch: "feature/dark", Message: "Add dark mode", Timestamp: at(6)},
		{Hash: "post00000001", RepoURL: repo, Branch: "gitmsg/social", Message: "Hello\n\nGitMsg: ext=\"social\"; type=\"post\"; v=\"0.1.0\"", Timestamp: at(5)},
		{Hash: "edit00000001", RepoURL: repo, Branch: "gitmsg/social", Message: "Hello again\n\nGitMsg: ext=\"social\"; type=\"post\"; edits=\"#commit:post00000001@gitmsg/social\"; v=\"0.1.0\"", Timestamp: at(7)},
		{Hash: "conf00000001", RepoURL: repo, Branch: "refs/gitmsg/social/config", Message: "config", Timestamp: at(8)},
		{Hash: "old000000001", RepoURL: repo, Branch: "old", Message: "Rebased away", Timestamp: at(9)},
	}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
	if _, err := MarkCommitsStale(repo, "old", map[string]bool{}); err != nil {
		t.Fatalf("MarkCommitsStale() error = %v", err)
	}
	if err := ExecLocked(func(db *sql.DB) error {
		_, err := UpsertVirtualCommit(db, VirtualCommit{RepoURL: repo, Hash: "ghost0000001", Branch: "ghost", Message: "Referenced only", Timestamp: at(9)})
		return err
	}); err != nil {
		t.Fatalf("UpsertVirtualCommit() error = %v", err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "repositories")); err != nil {
		t.Fatalf("RemoveAll() error = %v", err)
	}

	got, err := GetRepositoryBranches(repo)
	if err != nil {
		t.Fatalf("GetRepositoryBranches() error = %v", err)
	}
	want := []BranchSummary{
		{Name: "feature/dark", Commits: 1, LastTime: at(6)},
		{Name: "main", Commits: 2, LastTime: at(4)},
		{Name: "gitmsg/social", Commits: 1, LastTime: at(5)},
	}
	if len(got) != len(want) {
		t.Fatalf("branches = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("branch %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
