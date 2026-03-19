// fixture.go - Test repo setup and data seeding across all extensions
package test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	osexec "os/exec"
	"testing"
	"time"

	"github.com/gitsocial-org/gitsocial/core/cache"
	"github.com/gitsocial-org/gitsocial/core/fetch"
	"github.com/gitsocial-org/gitsocial/core/git"
	"github.com/gitsocial-org/gitsocial/core/gitmsg"
	"github.com/gitsocial-org/gitsocial/core/result"
	"github.com/gitsocial-org/gitsocial/extensions/pm"
	"github.com/gitsocial-org/gitsocial/extensions/release"
	"github.com/gitsocial-org/gitsocial/extensions/review"
	"github.com/gitsocial-org/gitsocial/extensions/social"
)

const (
	fixtureRepoTar  = "testdata/fixture-repo.tar.gz"
	fixtureMetaJSON = "testdata/fixture.json"
)

// Fixture holds references to seeded data for assertion in tests.
type Fixture struct {
	Workdir  string `json:"-"`
	CacheDir string `json:"-"`

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
}

// setupFixtureForMain loads the pre-built fixture repo from tarball.
// Falls back to generating fresh if tarball doesn't exist.
func setupFixtureForMain() *Fixture {
	if _, err := os.Stat(fixtureRepoTar); err != nil {
		panic(fmt.Sprintf("fixture tarball not found at %s — run: go test ./tui/test/ -run GenerateFixture -generate", fixtureRepoTar))
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
	// Reset all commit timestamps to "now" so relative time is always "just now",
	// regardless of when the fixture tarball was generated.
	resetTimestampsPanic()
	return &f
}

// SetupFixture creates a fresh fixture per test. Use getFixture(t) for shared read-only fixture.
func SetupFixture(t *testing.T) *Fixture {
	t.Helper()
	f := setupFixtureForMain()
	t.Cleanup(func() {
		cache.Reset()
		os.RemoveAll(f.Workdir)
		os.RemoveAll(f.CacheDir)
	})
	return f
}

// --- Generation (creates the repo from scratch) ---

// generateFixture creates a fresh fixture repo and saves it as tarball + metadata.
func generateFixture() {
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
	for _, ext := range []string{"social", "pm", "review", "release"} {
		if err := gitmsg.WriteExtConfig(resolved, ext, map[string]interface{}{
			"branch": "gitmsg/" + ext,
		}); err != nil {
			panic(fmt.Sprintf("WriteExtConfig %s: %v", ext, err))
		}
	}
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
	// Save tarball
	if err := os.MkdirAll("testdata", 0755); err != nil {
		panic(fmt.Sprintf("mkdir testdata: %v", err))
	}
	cmd := osexec.Command("tar", "czf", fixtureRepoTar, "-C", resolved, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("tar create: %v: %s", err, out))
	}
	// Save metadata
	metaBytes, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		panic(fmt.Sprintf("json marshal: %v", err))
	}
	if err := os.WriteFile(fixtureMetaJSON, metaBytes, 0644); err != nil {
		panic(fmt.Sprintf("write metadata: %v", err))
	}
	fmt.Printf("Generated fixture: %s (%s)\n", fixtureRepoTar, fixtureMetaJSON)
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
	social.CreateRepost(workdir, post.Data.ID)
	f.QuoteContent = "Adding my thoughts on this..."
	social.CreateQuote(workdir, post.Data.ID, f.QuoteContent)
	f.EditedContent = "Hello world! (updated)"
	social.EditPost(workdir, post.Data.ID, f.EditedContent)
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

// resetTimestampsPanic sets all cached commit timestamps to now.
func resetTimestampsPanic() {
	now := time.Now().UTC().Format(time.RFC3339)
	if err := cache.ExecLocked(func(db *sql.DB) error {
		_, err := db.Exec(`UPDATE core_commits SET timestamp = ?`, now)
		return err
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
