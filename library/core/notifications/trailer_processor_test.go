// trailer_processor_test.go - Tests from a trailer-carrying commit to its trailer ref and its notification
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
	cases := []struct {
		name, issueBranch, fixBranch string
	}{
		{"one branch", "main", "main"},
		{"branchless trailer on a code branch", "gitmsg/pm", "feature/x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupTestDB(t)
			const repoURL = "https://github.com/test/trailers"

			dir := t.TempDir()
			if err := git.Init(dir, "main"); err != nil {
				t.Fatalf("git.Init() error = %v", err)
			}

			setGitUser(t, dir, "Alice", "alice@example.com")
			issueContent := "Login button does nothing\n\n" + `GitMsg: ext="pm"; type="issue"; state="open"; v="0.1.0"`
			if _, err := git.CreateCommit(dir, git.CommitOptions{Message: issueContent, AllowEmpty: true}); err != nil {
				t.Fatalf("CreateCommit(issue) error = %v", err)
			}
			issueCommits, err := git.GetCommits(dir, &git.GetCommitsOptions{Branch: "main"})
			if err != nil || len(issueCommits) != 1 {
				t.Fatalf("GetCommits(issue) = %d commits, err = %v", len(issueCommits), err)
			}
			issueHash := issueCommits[0].Hash
			if _, err := fetch.ProcessCommits(dir, issueCommits, repoURL, tc.issueBranch, []fetch.CommitProcessor{TrailerProcessor()}); err != nil {
				t.Fatalf("ProcessCommits(issue) error = %v", err)
			}

			setGitUser(t, dir, "Bob", "bob@example.com")
			fixContent := "Wire up the login handler\n\nCloses: #commit:" + issueHash
			if _, err := git.CreateCommit(dir, git.CommitOptions{Message: fixContent, AllowEmpty: true}); err != nil {
				t.Fatalf("CreateCommit(fix) error = %v", err)
			}
			commits, err := git.GetCommits(dir, &git.GetCommitsOptions{Branch: "main"})
			if err != nil || len(commits) != 2 {
				t.Fatalf("GetCommits() = %d commits, err = %v", len(commits), err)
			}
			if _, err := fetch.ProcessCommits(dir, commits[:1], repoURL, tc.fixBranch, []fetch.CommitProcessor{TrailerProcessor()}); err != nil {
				t.Fatalf("ProcessCommits(fix) error = %v", err)
			}

			refs, err := cache.GetTrailerRefsTo(repoURL, issueHash)
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
			if refs[0].RepoURL != repoURL || refs[0].Branch != tc.fixBranch {
				t.Errorf("ref location = %s@%s, want %s@%s", refs[0].RepoURL, refs[0].Branch, repoURL, tc.fixBranch)
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
		})
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

	refs, err := cache.GetTrailerRefsTo(repoURL, targetHash)
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

// seedTrailer caches a commit of Bob that names target in a trailer, as the processor stores it with refBranch.
func seedTrailer(t *testing.T, repoURL, hash, branch, target, refBranch string) {
	t.Helper()
	if err := cache.ExecLocked(func(db *sql.DB) error {
		if _, err := db.Exec(`INSERT INTO core_commits (repo_url, hash, branch, author_name, author_email, message, timestamp) VALUES (?, ?, ?, 'Bob', 'bob@example.com', 'fix', '2025-10-21T12:00:00Z')`,
			repoURL, hash, branch); err != nil {
			return err
		}
		_, err := db.Exec(`INSERT INTO core_trailer_refs (repo_url, hash, branch, ref_repo_url, ref_hash, ref_branch, trailer_key, trailer_value) VALUES (?, ?, ?, ?, ?, ?, 'Closes', ?)`,
			repoURL, hash, branch, repoURL, target, refBranch, "#commit:"+target)
		return err
	}); err != nil {
		t.Fatalf("seed trailer: %v", err)
	}
}

// seedTarget caches one row of a commit that Alice authored, stale when stale is set.
func seedTarget(t *testing.T, repoURL, hash, branch string, stale bool) {
	t.Helper()
	if err := cache.ExecLocked(func(db *sql.DB) error {
		var staleSince interface{}
		if stale {
			staleSince = "2025-10-21T00:00:00Z"
		}
		_, err := db.Exec(`INSERT INTO core_commits (repo_url, hash, branch, author_name, author_email, message, timestamp, stale_since) VALUES (?, ?, ?, 'Alice', 'alice@example.com', 'target', '2025-10-20T12:00:00Z', ?)`,
			repoURL, hash, branch, staleSince)
		return err
	}); err != nil {
		t.Fatalf("seed target: %v", err)
	}
}

// TestTrailerProvider_branchlessTrailer pins invariant 7: a trailer stored with the code branch notifies the author of the issue on gitmsg/pm.
func TestTrailerProvider_branchlessTrailer(t *testing.T) {
	setupTestDB(t)
	workdir := setupGitRepo(t)
	const repoURL = "https://github.com/test/branchless"
	seedTarget(t, repoURL, "a55e00000001", "gitmsg/pm", false)
	seedTrailer(t, repoURL, "b0b000000001", "feature/x", "a55e00000001", "feature/x")

	p := &trailerProvider{}
	items, err := p.GetNotifications(workdir, Filter{})
	if err != nil {
		t.Fatalf("GetNotifications() error = %v", err)
	}
	if len(items) != 1 || items[0].Hash != "b0b000000001" {
		t.Errorf("notifications = %+v, want the one from the fix", items)
	}
}

// TestTrailerProvider_oneEntryPerTrailer pins invariant 8: a target with a stale and a live row gives one notification and one entry.
func TestTrailerProvider_oneEntryPerTrailer(t *testing.T) {
	setupTestDB(t)
	workdir := setupGitRepo(t)
	const repoURL = "https://github.com/test/moved-target"
	seedTarget(t, repoURL, "c0de00000001", "feature/x", true)
	seedTarget(t, repoURL, "c0de00000001", "main", false)
	seedTrailer(t, repoURL, "b0b000000002", "main", "c0de00000001", "feature/x")

	p := &trailerProvider{}
	items, err := p.GetNotifications(workdir, Filter{})
	if err != nil {
		t.Fatalf("GetNotifications() error = %v", err)
	}
	if len(items) != 1 {
		t.Errorf("notifications = %d, want 1", len(items))
	}
	count, err := p.GetUnreadCount(workdir)
	if err != nil {
		t.Fatalf("GetUnreadCount() error = %v", err)
	}
	if count != 1 {
		t.Errorf("unread count = %d, want 1", count)
	}
	refs, err := cache.GetTrailerRefsTo(repoURL, "c0de00000001")
	if err != nil {
		t.Fatalf("GetTrailerRefsTo() error = %v", err)
	}
	if len(refs) != 1 {
		t.Errorf("trailer refs = %d, want 1", len(refs))
	}
}
