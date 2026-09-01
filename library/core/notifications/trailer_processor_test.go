// trailer_processor_test.go - End-to-end test from a real trailer-carrying commit to its trailer ref and notification
package notifications

import (
	"database/sql"
	"testing"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/fetch"
	"github.com/gitsocial-org/gitsocial/library/core/git"
)

// setGitUser points the repo's committer identity at one address.
func setGitUser(t *testing.T, dir, name, email string) {
	t.Helper()
	git.ExecGit(dir, []string{"config", "user.name", name})
	git.ExecGit(dir, []string{"config", "user.email", email})
}

// TestTrailerProcessor_endToEnd walks a real commit carrying a `Closes:` trailer
// through ProcessCommits into core_trailer_refs and out again as a notification.
func TestTrailerProcessor_endToEnd(t *testing.T) {
	setupTestDB(t)

	const repoURL = "https://github.com/test/trailers"
	const branch = "main"

	dir := t.TempDir()
	if err := git.Init(dir, branch); err != nil {
		t.Fatalf("git.Init() error = %v", err)
	}

	setGitUser(t, dir, "Alice", "alice@example.com")
	issueContent := "Login button does nothing\n\n" + `GitMsg: ext="pm"; type="issue"; state="open"; v="0.1.0"`
	if _, err := git.CreateCommit(dir, git.CommitOptions{Message: issueContent, AllowEmpty: true}); err != nil {
		t.Fatalf("CreateCommit(issue) error = %v", err)
	}
	issueCommits, err := git.GetCommits(dir, &git.GetCommitsOptions{Branch: branch})
	if err != nil || len(issueCommits) != 1 {
		t.Fatalf("GetCommits(issue) = %d commits, err = %v", len(issueCommits), err)
	}
	issueHash := issueCommits[0].Hash

	setGitUser(t, dir, "Bob", "bob@example.com")
	fixContent := "Wire up the login handler\n\nCloses: #commit:" + issueHash
	if _, err := git.CreateCommit(dir, git.CommitOptions{Message: fixContent, AllowEmpty: true}); err != nil {
		t.Fatalf("CreateCommit(fix) error = %v", err)
	}

	commits, err := git.GetCommits(dir, &git.GetCommitsOptions{Branch: branch})
	if err != nil {
		t.Fatalf("GetCommits() error = %v", err)
	}
	if _, err := fetch.ProcessCommits(dir, commits, repoURL, branch, []fetch.CommitProcessor{TrailerProcessor()}); err != nil {
		t.Fatalf("ProcessCommits() error = %v", err)
	}

	refs, err := cache.GetTrailerRefsTo(repoURL, issueHash, branch)
	if err != nil {
		t.Fatalf("GetTrailerRefsTo() error = %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("GetTrailerRefsTo() = %d refs, want 1", len(refs))
	}
	if refs[0].TrailerKey != "Closes" {
		t.Errorf("TrailerKey = %q, want Closes", refs[0].TrailerKey)
	}
	if refs[0].AuthorEmail != "bob@example.com" {
		t.Errorf("AuthorEmail = %q, want bob@example.com", refs[0].AuthorEmail)
	}
	if refs[0].RepoURL != repoURL || refs[0].Branch != branch {
		t.Errorf("ref location = %s@%s, want %s@%s", refs[0].RepoURL, refs[0].Branch, repoURL, branch)
	}

	// The issue author sees the reference as a notification; its author does not.
	setGitUser(t, dir, "Alice", "alice@example.com")
	items, err := GetAll(dir, Filter{})
	if err != nil {
		t.Fatalf("GetAll() error = %v", err)
	}
	var found *Notification
	for i := range items {
		if items[i].Source == "core" && items[i].Hash != issueHash {
			found = &items[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no core reference notification for the issue author, got %+v", items)
	}
	if found.Type != "reference" {
		t.Errorf("Type = %q, want reference", found.Type)
	}
	if found.Actor.Email != "bob@example.com" {
		t.Errorf("Actor.Email = %q, want bob@example.com", found.Actor.Email)
	}

	setGitUser(t, dir, "Bob", "bob@example.com")
	own, err := GetAll(dir, Filter{})
	if err != nil {
		t.Fatalf("GetAll(bob) error = %v", err)
	}
	for _, n := range own {
		if n.Source == "core" && n.Type == "reference" {
			t.Errorf("the referencing author should not be notified about their own commit: %+v", n)
		}
	}
}

// TestTrailerProcessor_skipsGitMsgCommits checks the processor leaves structured
// GitMsg commits alone, they carry their references in the header instead.
func TestTrailerProcessor_skipsGitMsgCommits(t *testing.T) {
	setupTestDB(t)

	const repoURL = "https://github.com/test/trailers-gitmsg"
	const branch = "gitmsg/pm"

	dir := t.TempDir()
	if err := git.Init(dir, "main"); err != nil {
		t.Fatalf("git.Init() error = %v", err)
	}
	setGitUser(t, dir, "Alice", "alice@example.com")
	if _, err := git.CreateCommit(dir, git.CommitOptions{Message: "target", AllowEmpty: true}); err != nil {
		t.Fatalf("CreateCommit(target) error = %v", err)
	}
	targets, _ := git.GetCommits(dir, &git.GetCommitsOptions{Branch: "main"})
	targetHash := targets[0].Hash

	content := "Structured comment\n\nCloses: #commit:" + targetHash + "\n\n" +
		`GitMsg: ext="pm"; type="issue"; state="open"; v="0.1.0"`
	if _, err := git.CreateCommit(dir, git.CommitOptions{Message: content, AllowEmpty: true}); err != nil {
		t.Fatalf("CreateCommit(gitmsg) error = %v", err)
	}
	commits, _ := git.GetCommits(dir, &git.GetCommitsOptions{Branch: "main"})
	if _, err := fetch.ProcessCommits(dir, commits, repoURL, branch, []fetch.CommitProcessor{TrailerProcessor()}); err != nil {
		t.Fatalf("ProcessCommits() error = %v", err)
	}

	refs, err := cache.GetTrailerRefsTo(repoURL, targetHash, branch)
	if err != nil {
		t.Fatalf("GetTrailerRefsTo() error = %v", err)
	}
	if len(refs) != 0 {
		t.Errorf("GetTrailerRefsTo() = %d refs, want 0 for a GitMsg commit", len(refs))
	}
}

// TestTrailerProcessor_ignoresNonRefValues checks opaque trailer values (URLs,
// tracker ids) produce no trailer ref row.
func TestTrailerProcessor_ignoresNonRefValues(t *testing.T) {
	setupTestDB(t)

	const repoURL = "https://github.com/test/trailers-opaque"
	const branch = "main"

	dir := t.TempDir()
	if err := git.Init(dir, branch); err != nil {
		t.Fatalf("git.Init() error = %v", err)
	}
	setGitUser(t, dir, "Alice", "alice@example.com")
	if _, err := git.CreateCommit(dir, git.CommitOptions{
		Message:    "Fix it\n\nCloses: https://github.com/other/repo/issues/7\nRefs: TRACKER-42",
		AllowEmpty: true,
	}); err != nil {
		t.Fatalf("CreateCommit() error = %v", err)
	}
	commits, _ := git.GetCommits(dir, &git.GetCommitsOptions{Branch: branch})
	if _, err := fetch.ProcessCommits(dir, commits, repoURL, branch, []fetch.CommitProcessor{TrailerProcessor()}); err != nil {
		t.Fatalf("ProcessCommits() error = %v", err)
	}

	count, err := cache.QueryLocked(func(db *sql.DB) (int, error) {
		var n int
		err := db.QueryRow(`SELECT COUNT(*) FROM core_trailer_refs WHERE repo_url = ?`, repoURL).Scan(&n)
		return n, err
	})
	if err != nil {
		t.Fatalf("count trailer refs error = %v", err)
	}
	if count != 0 {
		t.Errorf("core_trailer_refs rows = %d, want 0 for opaque values", count)
	}
}
