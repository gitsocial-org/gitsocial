// trailer_refs_test.go - Tests for the trailer reference reader
package cache

import (
	"database/sql"
	"testing"
	"time"
)

func TestGetTrailerRefsTo_excludesStaleSource(t *testing.T) {
	setupTestDB(t)
	repoURL := "https://github.com/user/repo"
	now := time.Now()
	if err := InsertCommits([]Commit{
		{Hash: "111111111111", RepoURL: repoURL, Branch: "gitmsg/pm", Message: "an issue", Timestamp: now},
		{Hash: "aaa111111111", RepoURL: repoURL, Branch: "feature/x", Message: "fix", Timestamp: now},
		{Hash: "aaa111111111", RepoURL: repoURL, Branch: "main", Message: "fix", Timestamp: now},
	}); err != nil {
		t.Fatalf("InsertCommits() error = %v", err)
	}
	if err := ExecLocked(func(db *sql.DB) error {
		for _, branch := range []string{"feature/x", "main"} {
			if _, err := db.Exec(`INSERT INTO core_trailer_refs (repo_url, hash, branch, ref_repo_url, ref_hash, ref_branch, trailer_key, trailer_value) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				repoURL, "aaa111111111", branch, repoURL, "111111111111", "gitmsg/pm", "Closes", "#commit:111111111111@gitmsg/pm"); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("insert trailer refs: %v", err)
	}
	if _, err := MarkCommitsStaleByHome(repoURL, map[string]string{"aaa111111111": "main", "111111111111": "gitmsg/pm"}, nil); err != nil {
		t.Fatalf("MarkCommitsStaleByHome() error = %v", err)
	}
	refs, err := GetTrailerRefsTo(repoURL, "111111111111", "gitmsg/pm")
	if err != nil {
		t.Fatalf("GetTrailerRefsTo() error = %v", err)
	}
	if len(refs) != 1 || refs[0].Branch != "main" {
		t.Errorf("trailer refs = %+v, want the one from the live row under main", refs)
	}
}
