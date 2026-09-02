// fixture.go - Test repo setup and data seeding across all extensions
package test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/library/core/cache"
	"github.com/gitsocial-org/gitsocial/library/core/fetch"
	"github.com/gitsocial-org/gitsocial/library/core/git"
	"github.com/gitsocial-org/gitsocial/library/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/library/core/result"
	"github.com/gitsocial-org/gitsocial/library/extensions/memo"
	"github.com/gitsocial-org/gitsocial/library/extensions/pm"
	"github.com/gitsocial-org/gitsocial/library/extensions/release"
	"github.com/gitsocial-org/gitsocial/library/extensions/review"
	"github.com/gitsocial-org/gitsocial/library/extensions/social"
)

const (
	fixtureRepoTar  = "testdata/fixture-repo.tar.gz"
	fixtureForkTar  = "testdata/fixture-fork.tar.gz"
	fixtureMetaJSON = "testdata/fixture.json"
	// fixtureForkURL is the origin of the second (fork) repo. It is a fake URL:
	// nothing ever fetches it, and no forge adapter matches it, so the suite
	// stays offline.
	fixtureForkURL = "https://github.com/bob/repo"
	// fixtureInheritURL is a memo inherit source: a ref in the workspace repo,
	// so it survives the tarball without needing a repo behind it.
	fixtureInheritURL = "https://github.com/acme/handbook"
)

// sharedFixture is the per-package fixture created by TestMain and handed to
// read-only tests by getFixture.
var sharedFixture *Fixture

// Fixture holds references to seeded data for assertion in tests.
type Fixture struct {
	Workdir  string `json:"-"`
	CacheDir string `json:"-"`
	ForkDir  string `json:"-"`

	// Social
	PostID         string `json:"post_id"`
	PostContent    string `json:"post_content"`
	CommentContent string `json:"comment_content"`
	QuoteContent   string `json:"quote_content"`
	EditedContent  string `json:"edited_content"`

	// PM
	IssueID        string `json:"issue_id"`
	IssueSubject   string `json:"issue_subject"`
	MilestoneID    string `json:"milestone_id"`
	MilestoneTitle string `json:"milestone_title"`
	SprintID       string `json:"sprint_id"`
	SprintTitle    string `json:"sprint_title"`

	// Release
	ReleaseID      string `json:"release_id"`
	ReleaseSubject string `json:"release_subject"`
	ReleaseTag     string `json:"release_tag"`

	// Review
	PRID      string `json:"pr_id"`
	PRSubject string `json:"pr_subject"`

	// Memo (project tier, the only tier that lives inside the repo)
	MemoID         string `json:"memo_id"`
	MemoSubject    string `json:"memo_subject"`
	MemoBody       string `json:"memo_body"`
	MemoLabel      string `json:"memo_label"`
	EditedMemoID   string `json:"edited_memo_id"`
	EditedMemoSubj string `json:"edited_memo_subject"`
	InheritURL     string `json:"inherit_url"`

	// Forks
	ForkURL string `json:"fork_url"`

	// Cross-repo proposal: the fork's inert edit of the workspace issue
	ProposalRepoURL string `json:"proposal_repo_url"`
	ProposalIssueID string `json:"proposal_issue_id"`
}

// setupFixtureForMain loads the pre-built workspace and fork repos from their
// tarballs, opens a fresh cache and syncs both into it. Panics if a tarball is
// missing: regenerate with the -generate flag.
func setupFixtureForMain() *Fixture {
	for _, tarball := range []string{fixtureRepoTar, fixtureForkTar} {
		if _, err := os.Stat(tarball); err != nil {
			panic(fmt.Sprintf("fixture tarball not found at %s — run: go test ./tui/test/ -run GenerateFixture -generate", tarball))
		}
	}
	metaBytes, err := os.ReadFile(fixtureMetaJSON)
	if err != nil {
		panic(fmt.Sprintf("fixture metadata not found at %s — run: go test ./tui/test/ -run GenerateFixture -generate", fixtureMetaJSON))
	}
	var f Fixture
	if err := json.Unmarshal(metaBytes, &f); err != nil {
		panic(fmt.Sprintf("invalid fixture metadata: %v", err))
	}
	workdir, err := os.MkdirTemp("", "tui-test-*")
	if err != nil {
		panic(fmt.Sprintf("MkdirTemp: %v", err))
	}
	// Extract tarball into workdir
	cmd := osexec.Command("tar", "xzf", fixtureRepoTar, "-C", workdir)
	if out, err := cmd.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("tar extract: %v: %s", err, out))
	}
	resolved, _ := git.GetRootDir(workdir)
	if resolved == "" {
		resolved = workdir
	}
	f.Workdir = resolved
	cacheDir, err := os.MkdirTemp("", "tui-test-cache-*")
	if err != nil {
		panic(fmt.Sprintf("MkdirTemp cache: %v", err))
	}
	if err := cache.Open(cacheDir); err != nil {
		panic(fmt.Sprintf("cache.Open: %v", err))
	}
	f.CacheDir = cacheDir
	syncAllPanic(resolved)
	f.ForkDir = extractForkPanic()
	// The fork's cross-repo edit only resolves once the canonical it edits is
	// cached, so the fork is synced after the workspace.
	syncAllPanic(f.ForkDir)
	// Rewrite commit timestamps so relative time is always "just now",
	// regardless of when the fixture tarballs were generated.
	resetTimestampsPanic()
	return &f
}

// SetupFixture creates a fresh fixture per test. Use getFixture(t) for shared read-only fixture.
func SetupFixture(t *testing.T) *Fixture {
	t.Helper()
	// The cache handle is process-global and cache.Open is a no-op while one is
	// already open, so the shared fixture's cache must be closed first or this
	// "isolated" fixture would silently read and write the shared one.
	cache.Reset()
	f := setupFixtureForMain()
	t.Cleanup(func() {
		cache.Reset()
		os.RemoveAll(f.Workdir)
		os.RemoveAll(f.CacheDir)
		os.RemoveAll(f.ForkDir)
		// The cache handle is process-global: reopen the shared fixture's cache
		// so tests that run after this one still see their data.
		if sharedFixture != nil {
			if err := cache.Open(sharedFixture.CacheDir); err != nil {
				t.Logf("reopen shared cache: %v", err)
			}
		}
	})
	return f
}

// extractForkPanic unpacks the fork repo tarball into a fresh temp dir.
func extractForkPanic() string {
	dir, err := os.MkdirTemp("", "tui-test-fork-*")
	if err != nil {
		panic(fmt.Sprintf("MkdirTemp fork: %v", err))
	}
	cmd := osexec.Command("tar", "xzf", fixtureForkTar, "-C", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("tar extract fork: %v: %s", err, out))
	}
	resolved, _ := git.GetRootDir(dir)
	if resolved == "" {
		resolved = dir
	}
	return resolved
}

// --- Generation (creates the repo from scratch) ---

// generateFixture creates a fresh fixture repo and saves it as tarball + metadata.
func generateFixture() {
	restoreHome := isolateHomePanic()
	defer restoreHome()
	workdir, err := os.MkdirTemp("", "tui-fixture-gen-*")
	if err != nil {
		panic(fmt.Sprintf("MkdirTemp: %v", err))
	}
	defer os.RemoveAll(workdir)
	if err := git.Init(workdir, "main"); err != nil {
		panic(fmt.Sprintf("git.Init: %v", err))
	}
	resolved, _ := git.GetRootDir(workdir)
	if resolved == "" {
		resolved = workdir
	}
	if _, err := git.ExecGit(resolved, []string{"config", "user.email", "alice@example.com"}); err != nil {
		panic(fmt.Sprintf("git config email: %v", err))
	}
	if _, err := git.ExecGit(resolved, []string{"config", "user.name", "Alice"}); err != nil {
		panic(fmt.Sprintf("git config name: %v", err))
	}
	if _, err := git.CreateCommit(resolved, git.CommitOptions{Message: "Initial commit", AllowEmpty: true}); err != nil {
		panic(fmt.Sprintf("CreateCommit: %v", err))
	}
	if _, err := git.ExecGit(resolved, []string{"remote", "add", "origin", "https://github.com/user/repo"}); err != nil {
		panic(fmt.Sprintf("git remote add: %v", err))
	}
	writeExtConfigsPanic(resolved)
	// Open a temporary cache — extension APIs write to both git and cache
	cacheDir, err := os.MkdirTemp("", "tui-fixture-cache-*")
	if err != nil {
		panic(fmt.Sprintf("MkdirTemp cache: %v", err))
	}
	defer os.RemoveAll(cacheDir)
	if err := cache.Open(cacheDir); err != nil {
		panic(fmt.Sprintf("cache.Open: %v", err))
	}
	defer cache.Reset()
	f := &Fixture{}
	f.seedSocialPanic(resolved)
	f.seedPMPanic(resolved)
	f.seedReleasePanic(resolved)
	f.seedReviewPanic(resolved)
	f.seedMemoPanic(resolved)
	f.seedForkPanic(resolved)
	forkDir := f.seedProposalPanic()
	defer os.RemoveAll(forkDir)
	// Save tarballs
	if err := os.MkdirAll("testdata", 0755); err != nil {
		panic(fmt.Sprintf("mkdir testdata: %v", err))
	}
	cmd := osexec.Command("tar", "czf", fixtureRepoTar, "-C", resolved, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("tar create: %v: %s", err, out))
	}
	forkCmd := osexec.Command("tar", "czf", fixtureForkTar, "-C", forkDir, ".")
	if out, err := forkCmd.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("tar create fork: %v: %s", err, out))
	}
	// Save metadata
	metaBytes, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		panic(fmt.Sprintf("json marshal: %v", err))
	}
	if err := os.WriteFile(fixtureMetaJSON, metaBytes, 0644); err != nil {
		panic(fmt.Sprintf("write metadata: %v", err))
	}
	fmt.Printf("Generated fixture: %s + %s (%s)\n", fixtureRepoTar, fixtureForkTar, fixtureMetaJSON)
}

// isolateHomePanic points HOME, XDG_CONFIG_HOME and GITSOCIAL_PERSONAL_REPO at
// a throwaway directory for the duration of fixture generation, so nothing can
// reach the real personal memo repo or the real config. Returns the restore fn.
func isolateHomePanic() func() {
	tmpHome, err := os.MkdirTemp("", "tui-fixture-home-*")
	if err != nil {
		panic(fmt.Sprintf("MkdirTemp home: %v", err))
	}
	saved := map[string]string{}
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "GITSOCIAL_PERSONAL_REPO"} {
		saved[key] = os.Getenv(key)
	}
	os.Setenv("HOME", tmpHome)
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpHome, ".config"))
	os.Setenv("GITSOCIAL_PERSONAL_REPO", filepath.Join(tmpHome, "personal"))
	return func() {
		for key, val := range saved {
			if val == "" {
				os.Unsetenv(key)
			} else {
				os.Setenv(key, val)
			}
		}
		os.RemoveAll(tmpHome)
	}
}

// writeExtConfigsPanic writes the gitmsg extension config ref for every
// extension the fixture seeds.
func writeExtConfigsPanic(workdir string) {
	for _, ext := range []string{"social", "pm", "review", "release", "memo"} {
		if err := gitmsg.WriteExtConfig(workdir, ext, map[string]interface{}{
			"branch": "gitmsg/" + ext,
		}); err != nil {
			panic(fmt.Sprintf("WriteExtConfig %s: %v", ext, err))
		}
	}
}

// --- Seeding ---

func (f *Fixture) seedSocialPanic(workdir string) {
	f.PostContent = "Hello world!"
	post := social.CreatePost(workdir, f.PostContent, nil)
	mustSucceed("CreatePost", post.Success, resultErrMsg(post.Error))
	f.PostID = post.Data.ID
	social.CreatePost(workdir, "Git-native collaboration is the future", nil)
	f.CommentContent = "Great idea!"
	social.CreateComment(workdir, post.Data.ID, f.CommentContent, nil)
	social.CreateRepost(workdir, post.Data.ID, nil)
	f.QuoteContent = "Adding my thoughts on this..."
	social.CreateQuote(workdir, post.Data.ID, f.QuoteContent, nil)
	f.EditedContent = "Hello world! (updated)"
	social.EditPost(workdir, post.Data.ID, f.EditedContent, nil)
}

func (f *Fixture) seedPMPanic(workdir string) {
	f.IssueSubject = "Add dark mode support"
	issue1 := pm.CreateIssue(workdir, f.IssueSubject, "Users can toggle between light and dark themes in settings.", pm.CreateIssueOptions{})
	mustSucceed("CreateIssue", issue1.Success, resultErrMsg(issue1.Error))
	f.IssueID = issue1.Data.ID
	issue2 := pm.CreateIssue(workdir, "Add keyboard shortcuts", "Dashboard needs keyboard navigation", pm.CreateIssueOptions{})
	if issue2.Success {
		pm.CloseIssue(workdir, issue2.Data.ID)
	}
	pm.CreateIssue(workdir, "Implement real-time notifications", "No longer needed", pm.CreateIssueOptions{
		State: pm.StateCancelled,
	})
	f.MilestoneTitle = "Release v2.0"
	ms := pm.CreateMilestone(workdir, f.MilestoneTitle, "Dark mode and dashboard analytics.", pm.CreateMilestoneOptions{})
	if ms.Success {
		f.MilestoneID = ms.Data.ID
	}
	f.SprintTitle = "Sprint 23: UX Polish"
	now := time.Now()
	end := now.Add(14 * 24 * time.Hour)
	sp := pm.CreateSprint(workdir, f.SprintTitle, "Two-week sprint for user experience improvements.", pm.CreateSprintOptions{Start: now, End: end})
	if sp.Success {
		f.SprintID = sp.Data.ID
	}
}

func (f *Fixture) seedReleasePanic(workdir string) {
	f.ReleaseSubject = "Release v1.0.0"
	f.ReleaseTag = "v1.0.0"
	rel := release.CreateRelease(workdir, f.ReleaseSubject, "Pre-built binaries for Linux, macOS, and Windows.", release.CreateReleaseOptions{
		Tag: f.ReleaseTag, Version: "1.0.0", Artifacts: []string{"app-linux-x64.tar.gz", "app-darwin-arm64.tar.gz", "app-windows-x64.zip"},
	})
	if rel.Success {
		f.ReleaseID = rel.Data.ID
	}
	release.CreateRelease(workdir, "Release v2.0.0-beta.1", "Beta release for testing new features.", release.CreateReleaseOptions{
		Tag: "v2.0.0-beta.1", Version: "2.0.0-beta.1", Prerelease: true,
	})
}

func (f *Fixture) seedReviewPanic(workdir string) {
	if _, err := git.ExecGit(workdir, []string{"branch", "dark-mode"}); err != nil {
		panic(fmt.Sprintf("git branch dark-mode: %v", err))
	}
	if _, err := git.ExecGit(workdir, []string{"branch", "theme-toggle"}); err != nil {
		panic(fmt.Sprintf("git branch theme-toggle: %v", err))
	}
	f.PRSubject = "Add dark mode support"
	pr1 := review.CreatePR(workdir, f.PRSubject, "Implements theme toggle with system preference detection.", review.CreatePROptions{
		Base: "main", Head: "dark-mode",
	})
	mustSucceed("CreatePR", pr1.Success, resultErrMsg(pr1.Error))
	f.PRID = pr1.Data.ID
	review.CreateFeedback(workdir, "LGTM!", review.CreateFeedbackOptions{
		PullRequest: pr1.Data.ID, ReviewState: review.ReviewStateApproved,
	})
	pr2 := review.CreatePR(workdir, "Add theme toggle component", "Clean separation of theme variables.", review.CreatePROptions{
		Base: "main", Head: "theme-toggle",
	})
	if pr2.Success {
		review.MergePR(workdir, pr2.Data.ID, review.MergeStrategyMerge)
	}
}

// seedMemoPanic initializes the project memo tier and seeds project-tier memos
// plus one inherited source. Only the project tier is seeded: personal and
// session tiers live outside the repo, so they cannot travel in the tarball.
func (f *Fixture) seedMemoPanic(workdir string) {
	init := memo.InitProject(workdir)
	mustSucceed("memo.InitProject", init.Success, resultErrMsg(init.Error))
	f.MemoSubject = "Cache invalidation rules"
	f.MemoBody = "Storage can be deleted anytime; fetch strategy comes from cache.db metadata."
	f.MemoLabel = "kind/decision"
	m := memo.CreateMemo(workdir, f.MemoSubject, f.MemoBody, memo.CreateMemoOptions{
		Tier: memo.TierProject, Labels: []string{f.MemoLabel},
	})
	mustSucceed("memo.CreateMemo", m.Success, resultErrMsg(m.Error))
	f.MemoID = m.Data.ID
	f.EditedMemoSubj = "Review checklist (updated)"
	second := memo.CreateMemo(workdir, "Review checklist", "Check the spec before the code.", memo.CreateMemoOptions{
		Tier: memo.TierProject, Labels: []string{"kind/howto"},
	})
	mustSucceed("memo.CreateMemo second", second.Success, resultErrMsg(second.Error))
	f.EditedMemoID = second.Data.ID
	edited := memo.EditMemo(workdir, second.Data.ID, memo.EditMemoOptions{Subject: &f.EditedMemoSubj})
	mustSucceed("memo.EditMemo", edited.Success, resultErrMsg(edited.Error))
	f.InheritURL = fixtureInheritURL
	inherit := memo.AddInherit(workdir, f.InheritURL)
	mustSucceed("memo.AddInherit", inherit.Success, resultErrMsg(inherit.Error))
}

// seedForkPanic registers the fork repo on the workspace.
func (f *Fixture) seedForkPanic(workdir string) {
	f.ForkURL = fixtureForkURL
	if err := gitmsg.AddFork(workdir, f.ForkURL); err != nil {
		panic(fmt.Sprintf("AddFork: %v", err))
	}
}

// seedProposalPanic builds the fork repo and has it close the workspace issue.
// A cross-repo edit is inert until the owner accepts it, so this leaves an open
// proposal on the issue. Returns the fork repo path for tarballing.
func (f *Fixture) seedProposalPanic() string {
	forkDir, err := os.MkdirTemp("", "tui-fixture-fork-*")
	if err != nil {
		panic(fmt.Sprintf("MkdirTemp fork: %v", err))
	}
	if err := git.Init(forkDir, "main"); err != nil {
		panic(fmt.Sprintf("git.Init fork: %v", err))
	}
	resolved, _ := git.GetRootDir(forkDir)
	if resolved == "" {
		resolved = forkDir
	}
	for _, kv := range [][2]string{{"user.email", "bob@example.com"}, {"user.name", "Bob"}} {
		if _, err := git.ExecGit(resolved, []string{"config", kv[0], kv[1]}); err != nil {
			panic(fmt.Sprintf("git config %s: %v", kv[0], err))
		}
	}
	if _, err := git.CreateCommit(resolved, git.CommitOptions{Message: "Initial commit", AllowEmpty: true}); err != nil {
		panic(fmt.Sprintf("CreateCommit fork: %v", err))
	}
	if _, err := git.ExecGit(resolved, []string{"remote", "add", "origin", fixtureForkURL}); err != nil {
		panic(fmt.Sprintf("git remote add fork: %v", err))
	}
	writeExtConfigsPanic(resolved)
	closed := pm.CloseIssue(resolved, f.IssueID)
	mustSucceed("CloseIssue from fork", closed.Success, resultErrMsg(closed.Error))
	f.ProposalRepoURL = fixtureForkURL
	f.ProposalIssueID = f.IssueID
	return resolved
}

// resetTimestampsPanic rewrites every cached commit timestamp into the last few
// seconds, preserving the original order.
func resetTimestampsPanic() {
	if err := cache.ExecLocked(func(db *sql.DB) error {
		rows, err := db.Query(`SELECT repo_url, hash, branch FROM core_commits ORDER BY timestamp ASC, rowid ASC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		var keys [][3]string
		for rows.Next() {
			var k [3]string
			if err := rows.Scan(&k[0], &k[1], &k[2]); err != nil {
				return err
			}
			keys = append(keys, k)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		// One second apart, ending now, so every commit still renders as
		// "just now" while the original order (and therefore version numbering
		// and list order) stays deterministic.
		base := time.Now().UTC().Add(-time.Duration(len(keys)) * time.Second)
		for i, k := range keys {
			ts := base.Add(time.Duration(i) * time.Second).Format(time.RFC3339)
			if _, err := db.Exec(
				`UPDATE core_commits SET timestamp = ? WHERE repo_url = ? AND hash = ? AND branch = ?`,
				ts, k[0], k[1], k[2],
			); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		panic(fmt.Sprintf("reset timestamps: %v", err))
	}
}

func syncAllPanic(workdir string) {
	_ = fetch.SyncWorkspace(workdir)
}

// mustSucceed panics if result failed — for use outside tests.
func mustSucceed(op string, ok bool, msg string) {
	if !ok {
		panic(fmt.Sprintf("%s failed: %s", op, msg))
	}
}

// resultErrMsg extracts the message from a *result.Error.
func resultErrMsg(e *result.Error) string {
	if e == nil {
		return "<nil>"
	}
	return e.Message
}

// CloneFixture copies a base repo for per-test isolation.
func CloneFixture(t *testing.T, baseDir string) string {
	t.Helper()
	dst := t.TempDir()
	cmd := osexec.Command("cp", "-a", baseDir+"/.", dst)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("CloneFixture: %v: %s", err, out)
	}
	resolved, err := git.GetRootDir(dst)
	if err == nil && resolved != "" {
		return resolved
	}
	return dst
}
